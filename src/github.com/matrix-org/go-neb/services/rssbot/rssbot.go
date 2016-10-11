package services

import (
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/mmcdole/gofeed"
	"html"
	"time"
)

const minPollingIntervalSeconds = 60 // 1 min (News feeds can be genuinely spammy)

type feedPoller struct{}

func (p *feedPoller) IntervalSecs() int64 { return 10 }
func (p *feedPoller) OnPoll(s types.Service, cli *matrix.Client) {
	logger := log.WithFields(log.Fields{
		"service_id":   s.ServiceID(),
		"service_type": s.ServiceType(),
	})

	frService, ok := s.(*rssBotService)
	if !ok {
		logger.Error("RSSBot: OnPoll called without a Feed Service instance")
		return
	}
	now := time.Now().Unix() // Second resolution

	// Work out which feeds should be polled
	var pollFeeds []string
	for u, feedInfo := range frService.Feeds {
		if feedInfo.NextPollTimestampSecs == 0 || now >= feedInfo.NextPollTimestampSecs {
			// re-query this feed
			pollFeeds = append(pollFeeds, u)
		}
	}

	if len(pollFeeds) == 0 {
		return
	}

	// Query each feed and send new items to subscribed rooms
	for _, u := range pollFeeds {
		feed, items, err := p.queryFeed(frService, u)
		if err != nil {
			logger.WithField("feed_url", u).WithError(err).Error("Failed to query feed")
			continue
		}
		// Loop backwards since [0] is the most recent and we want to send in chronological order
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if err := p.sendToRooms(frService, cli, u, feed, item); err != nil {
				logger.WithFields(log.Fields{
					"feed_url":   u,
					log.ErrorKey: err,
					"item":       item,
				}).Error("Failed to send item to room")
			}
		}
	}

	// Persist the service to save the next poll times
	if _, err := database.GetServiceDB().StoreService(frService); err != nil {
		logger.WithError(err).Error("Failed to persist next poll times for service")
	}
}

// Query the given feed, update relevant timestamps and return NEW items
func (p *feedPoller) queryFeed(s *rssBotService, feedURL string) (*gofeed.Feed, []gofeed.Item, error) {
	log.WithField("feed_url", feedURL).Info("Querying feed")
	var items []gofeed.Item
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, items, err
	}

	// Work out which items are new, if any (based on the last updated TS we have)
	// If the TS is 0 then this is the first ever poll, so let's not send 10s of events
	// into the room and just do new ones from this point onwards.
	if s.Feeds[feedURL].FeedUpdatedTimestampSecs != 0 {
		for _, i := range feed.Items {
			if i == nil || i.PublishedParsed == nil {
				continue
			}
			if i.PublishedParsed.Unix() > s.Feeds[feedURL].FeedUpdatedTimestampSecs {
				items = append(items, *i)
			}
		}
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

	p.updateFeedInfo(s, feedURL, nextPollTsSec, feedLastUpdatedTs)
	return feed, items, nil
}

func (p *feedPoller) updateFeedInfo(s *rssBotService, feedURL string, nextPollTs, feedUpdatedTs int64) {
	for u := range s.Feeds {
		if u != feedURL {
			continue
		}
		f := s.Feeds[u]
		f.NextPollTimestampSecs = nextPollTs
		f.FeedUpdatedTimestampSecs = feedUpdatedTs
		s.Feeds[u] = f
	}
}

func (p *feedPoller) sendToRooms(s *rssBotService, cli *matrix.Client, feedURL string, feed *gofeed.Feed, item gofeed.Item) error {
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

type rssBotService struct {
	types.DefaultService
	id            string
	serviceUserID string
	Feeds         map[string]struct { // feed_url => { }
		PollIntervalMins         int      `json:"poll_interval_mins"`
		Rooms                    []string `json:"rooms"`
		NextPollTimestampSecs    int64    // Internal: When we should poll again
		FeedUpdatedTimestampSecs int64    // Internal: The last time the feed was updated
	} `json:"feeds"`
}

func (s *rssBotService) ServiceUserID() string { return s.serviceUserID }
func (s *rssBotService) ServiceID() string     { return s.id }
func (s *rssBotService) ServiceType() string   { return "rssbot" }
func (s *rssBotService) Poller() types.Poller  { return &feedPoller{} }

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
		if _, err := fp.ParseURL(feedURL); err != nil {
			return fmt.Errorf("Failed to read URL %s: %s", feedURL, err.Error())
		}
		if len(feedInfo.Rooms) == 0 {
			return fmt.Errorf("Feed %s has no rooms to send updates to", feedURL)
		}
	}
	return nil
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

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		r := &rssBotService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
		return r
	})
}
