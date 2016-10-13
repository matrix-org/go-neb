package services

import (
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/die-net/lrucache"
	"github.com/gregjones/httpcache"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/mmcdole/gofeed"
	"html"
	"net/http"
	"time"
)

var cachingClient *http.Client

const minPollingIntervalSeconds = 60 * 5 // 5 min (News feeds can be genuinely spammy)

type rssBotService struct {
	types.DefaultService
	id            string
	serviceUserID string
	Feeds         map[string]struct { // feed_url => { }
		PollIntervalMins         int      `json:"poll_interval_mins"`
		Rooms                    []string `json:"rooms"`
		NextPollTimestampSecs    int64    // Internal: When we should poll again
		FeedUpdatedTimestampSecs int64    // Internal: The last time the feed was updated
		RecentGUIDs              []string // Internal: The most recently seen GUIDs. Sized to the number of items in the feed.
	} `json:"feeds"`
}

func (s *rssBotService) ServiceUserID() string { return s.serviceUserID }
func (s *rssBotService) ServiceID() string     { return s.id }
func (s *rssBotService) ServiceType() string   { return "rssbot" }

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *rssBotService) Register(oldService types.Service, client *matrix.Client) error {
	if len(s.Feeds) == 0 {
		// this is an error UNLESS the old service had some feeds in which case they are deleting us :(
		var numOldFeeds int
		oldFeedService, ok := oldService.(*rssBotService)
		if !ok {
			log.WithField("service", oldService).Error("Old service isn't a rssBotService")
		} else {
			numOldFeeds = len(oldFeedService.Feeds)
		}
		if numOldFeeds == 0 {
			return errors.New("An RSS feed must be specified.")
		}
		return nil
	}
	// Make sure we can parse the feed
	for feedURL, feedInfo := range s.Feeds {
		fp := gofeed.NewParser()
		fp.Client = cachingClient
		if _, err := fp.ParseURL(feedURL); err != nil {
			return fmt.Errorf("Failed to read URL %s: %s", feedURL, err.Error())
		}
		if len(feedInfo.Rooms) == 0 {
			return fmt.Errorf("Feed %s has no rooms to send updates to", feedURL)
		}
	}

	s.joinRooms(client)
	return nil
}

func (s *rssBotService) joinRooms(client *matrix.Client) {
	roomSet := make(map[string]bool)
	for _, feedInfo := range s.Feeds {
		for _, roomID := range feedInfo.Rooms {
			roomSet[roomID] = true
		}
	}

	for roomID := range roomSet {
		if _, err := client.JoinRoom(roomID, "", ""); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    roomID,
				"user_id":    client.UserID,
			}).Error("Failed to join room")
		}
	}
}

func (s *rssBotService) PostRegister(oldService types.Service) {
	if len(s.Feeds) == 0 { // bye-bye :(
		logger := log.WithFields(log.Fields{
			"service_id":   s.ServiceID(),
			"service_type": s.ServiceType(),
		})
		logger.Info("Deleting service: No feeds remaining.")
		polling.StopPolling(s)
		if err := database.GetServiceDB().DeleteService(s.ServiceID()); err != nil {
			logger.WithError(err).Error("Failed to delete service")
		}
	}
}

func (s *rssBotService) OnPoll(cli *matrix.Client) time.Time {
	logger := log.WithFields(log.Fields{
		"service_id":   s.ServiceID(),
		"service_type": s.ServiceType(),
	})
	now := time.Now().Unix() // Second resolution

	// Work out which feeds should be polled
	var pollFeeds []string
	for u, feedInfo := range s.Feeds {
		if feedInfo.NextPollTimestampSecs == 0 || now >= feedInfo.NextPollTimestampSecs {
			// re-query this feed
			pollFeeds = append(pollFeeds, u)
		}
	}

	if len(pollFeeds) == 0 {
		return s.nextTimestamp()
	}

	// Query each feed and send new items to subscribed rooms
	for _, u := range pollFeeds {
		feed, items, err := s.queryFeed(u)
		if err != nil {
			logger.WithField("feed_url", u).WithError(err).Error("Failed to query feed")
			continue
		}
		// Loop backwards since [0] is the most recent and we want to send in chronological order
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if err := s.sendToRooms(cli, u, feed, item); err != nil {
				logger.WithFields(log.Fields{
					"feed_url":   u,
					log.ErrorKey: err,
					"item":       item,
				}).Error("Failed to send item to room")
			}
		}
	}

	// Persist the service to save the next poll times
	if _, err := database.GetServiceDB().StoreService(s); err != nil {
		logger.WithError(err).Error("Failed to persist next poll times for service")
	}

	return s.nextTimestamp()
}

func (s *rssBotService) nextTimestamp() time.Time {
	// return the earliest next poll ts
	var earliestNextTs int64
	for _, feedInfo := range s.Feeds {
		if earliestNextTs == 0 || feedInfo.NextPollTimestampSecs < earliestNextTs {
			earliestNextTs = feedInfo.NextPollTimestampSecs
		}
	}
	return time.Unix(earliestNextTs, 0)
}

// Query the given feed, update relevant timestamps and return NEW items
func (s *rssBotService) queryFeed(feedURL string) (*gofeed.Feed, []gofeed.Item, error) {
	log.WithField("feed_url", feedURL).Info("Querying feed")
	var items []gofeed.Item
	fp := gofeed.NewParser()
	fp.Client = cachingClient
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, items, err
	}

	// Work out which items are new, if any (based on the last updated TS we have)
	// If the TS is 0 then this is the first ever poll, so let's not send 10s of events
	// into the room and just do new ones from this point onwards.
	if s.Feeds[feedURL].FeedUpdatedTimestampSecs != 0 {
		items = s.newItems(feedURL, feed.Items)
	}

	now := time.Now().Unix() // Second resolution

	// Work out when this feed was last updated
	var feedLastUpdatedTs int64
	if feed.UpdatedParsed != nil {
		feedLastUpdatedTs = feed.UpdatedParsed.Unix()
	} else if len(feed.Items) > 0 {
		i := feed.Items[0]
		if i != nil && i.PublishedParsed != nil {
			feedLastUpdatedTs = i.PublishedParsed.Unix()
		}
	}

	// Work out when to next poll this feed
	nextPollTsSec := now + minPollingIntervalSeconds
	if s.Feeds[feedURL].PollIntervalMins > int(minPollingIntervalSeconds/60) {
		nextPollTsSec = now + int64(s.Feeds[feedURL].PollIntervalMins*60)
	}
	// TODO: Handle the 'sy' Syndication extension to control update interval.
	// See http://www.feedforall.com/syndication.htm and http://web.resource.org/rss/1.0/modules/syndication/

	s.updateFeedInfo(feedURL, feed.Items, nextPollTsSec, feedLastUpdatedTs)
	return feed, items, nil
}

func (s *rssBotService) newItems(feedURL string, allItems []*gofeed.Item) (items []gofeed.Item) {
	for _, i := range allItems {
		if i == nil || i.PublishedParsed == nil {
			continue
		}

		if i.PublishedParsed.Unix() > s.Feeds[feedURL].FeedUpdatedTimestampSecs {
			// if we've seen this guid before, we've sent it before (even if the timestamp is newer)
			seenBefore := false
			for _, guid := range s.Feeds[feedURL].RecentGUIDs {
				if guid == i.GUID {
					seenBefore = true
					break
				}
			}
			if seenBefore {
				continue
			}

			items = append(items, *i)
		}
	}
	return
}

func (s *rssBotService) updateFeedInfo(feedURL string, allFeedItems []*gofeed.Item, nextPollTs, feedUpdatedTs int64) {
	// map items to guid strings
	var guids []string
	for _, i := range allFeedItems {
		guids = append(guids, i.GUID)
	}

	for u := range s.Feeds {
		if u != feedURL {
			continue
		}
		f := s.Feeds[u]
		f.NextPollTimestampSecs = nextPollTs
		f.FeedUpdatedTimestampSecs = feedUpdatedTs
		f.RecentGUIDs = guids
		s.Feeds[u] = f
	}
}

func (s *rssBotService) sendToRooms(cli *matrix.Client, feedURL string, feed *gofeed.Feed, item gofeed.Item) error {
	logger := log.WithField("feed_url", feedURL).WithField("title", item.Title)
	logger.Info("New feed item")
	for _, roomID := range s.Feeds[feedURL].Rooms {
		if _, err := cli.SendMessageEvent(roomID, "m.room.message", itemToHTML(feed, item)); err != nil {
			logger.WithError(err).WithField("room_id", roomID).Error("Failed to send to room")
		}
	}
	return nil
}

// SomeOne posted a new article: Title Of The Entry ( https://someurl.com/blag )
func itemToHTML(feed *gofeed.Feed, item gofeed.Item) matrix.HTMLMessage {
	return matrix.GetHTMLMessage("m.notice", fmt.Sprintf(
		"<i>%s</i> posted a new article: %s ( %s )",
		html.EscapeString(feed.Title), html.EscapeString(item.Title), html.EscapeString(item.Link),
	))
}

func init() {
	lruCache := lrucache.New(1024*1024*20, 0) // 20 MB cache, no max-age
	cachingClient = httpcache.NewTransport(lruCache).Client()
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		r := &rssBotService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
		return r
	})
}
