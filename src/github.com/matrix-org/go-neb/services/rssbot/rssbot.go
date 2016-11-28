// Package rssbot implements a Service capable of reading Atom/RSS feeds.
package rssbot

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/die-net/lrucache"
	"github.com/gregjones/httpcache"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/mmcdole/gofeed"
	"github.com/prometheus/client_golang/prometheus"
)

// ServiceType of the RSS Bot service
const ServiceType = "rssbot"

var cachingClient *http.Client

var (
	pollCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_rss_polls_total",
		Help: "The number of feed polls from RSS services",
	}, []string{"url", "http_status"})
)

const minPollingIntervalSeconds = 60 * 5 // 5 min (News feeds can be genuinely spammy)

// Service contains the Config fields for this service.
//
// Example request:
//   {
//       feeds: {
//           "http://rss.cnn.com/rss/edition.rss": {
//                poll_interval_mins: 60,
//                rooms: ["!cBrPbzWazCtlkMNQSF:localhost"]
//           },
//           "https://www.wired.com/feed/": {
//                rooms: ["!qmElAGdFYCHoCJuaNt:localhost"]
//           }
//       }
//   }
type Service struct {
	types.DefaultService
	// Feeds is a map of feed URL to configuration options for this feed.
	Feeds map[string]struct {
		// Optional. The time to wait between polls. If this is less than minPollingIntervalSeconds, it is ignored.
		PollIntervalMins int `json:"poll_interval_mins"`
		// The list of rooms to send feed updates into. This cannot be empty.
		Rooms []string `json:"rooms"`
		// True if rss bot is unable to poll this feed. This is populated by Go-NEB. Use /getService to
		// retrieve this value.
		IsFailing bool `json:"is_failing"`
		// The time of the last successful poll. This is populated by Go-NEB. Use /getService to retrieve
		// this value.
		FeedUpdatedTimestampSecs int64 `json:"last_updated_ts_secs"`
		// Internal field. When we should poll again.
		NextPollTimestampSecs int64
		// Internal field. The most recently seen GUIDs. Sized to the number of items in the feed.
		RecentGUIDs []string
	} `json:"feeds"`
}

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *Service) Register(oldService types.Service, client *matrix.Client) error {
	if len(s.Feeds) == 0 {
		// this is an error UNLESS the old service had some feeds in which case they are deleting us :(
		var numOldFeeds int
		oldFeedService, ok := oldService.(*Service)
		if !ok {
			log.WithField("service", oldService).Error("Old service isn't an rssbot.Service")
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

func (s *Service) joinRooms(client *matrix.Client) {
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

// PostRegister deletes this service if there are no feeds remaining.
func (s *Service) PostRegister(oldService types.Service) {
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

// OnPoll rechecks RSS feeds which are due to be polled.
//
// In order for a feed to be polled, the current time must be greater than NextPollTimestampSecs.
// In order for an item on a feed to be sent to Matrix, the item's GUID must not exist in RecentGUIDs.
// The GUID for an item is created according to the following rules:
//   - If there is a GUID field, use it.
//   - Else if there is a Link field, use it as the GUID.
//   - Else if there is a Title field, use it as the GUID.
//
// Returns a timestamp representing when this Service should have OnPoll called again.
func (s *Service) OnPoll(cli *matrix.Client) time.Time {
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
			incrementMetrics(u, err)
			continue
		}
		incrementMetrics(u, nil)
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

func incrementMetrics(urlStr string, err error) {
	// extract domain part of RSS feed URL to get coarser (more useful) statistics
	domain := urlStr
	u, urlErr := url.Parse(urlStr)
	if urlErr == nil {
		domain = u.Host
	}
	if err != nil {
		herr, ok := err.(gofeed.HTTPError)
		statusCode := 0 // e.g. network timeout
		if ok {
			statusCode = herr.StatusCode
		}
		pollCounter.With(prometheus.Labels{"url": domain, "http_status": strconv.Itoa(statusCode)}).Inc()
	} else {
		pollCounter.With(prometheus.Labels{"url": domain, "http_status": "200"}).Inc() // technically 2xx but gofeed doesn't tell us which
	}
}

func (s *Service) nextTimestamp() time.Time {
	// return the earliest next poll ts
	var earliestNextTs int64
	for _, feedInfo := range s.Feeds {
		if earliestNextTs == 0 || feedInfo.NextPollTimestampSecs < earliestNextTs {
			earliestNextTs = feedInfo.NextPollTimestampSecs
		}
	}

	// Don't allow times in the past. Set a min re-poll threshold of 60s to avoid
	// tight-looping on feeds which 500.
	now := time.Now().Unix()
	if earliestNextTs <= now {
		earliestNextTs = now + 60
	}

	return time.Unix(earliestNextTs, 0)
}

// Query the given feed, update relevant timestamps and return NEW items
func (s *Service) queryFeed(feedURL string) (*gofeed.Feed, []gofeed.Item, error) {
	log.WithField("feed_url", feedURL).Info("Querying feed")
	var items []gofeed.Item
	fp := gofeed.NewParser()
	fp.Client = cachingClient
	feed, err := fp.ParseURL(feedURL)
	// check for no items in addition to any returned errors as it appears some RSS feeds
	// do not consistently return items.
	if err != nil || len(feed.Items) == 0 {
		f := s.Feeds[feedURL]
		f.IsFailing = true
		s.Feeds[feedURL] = f
		return nil, items, err
	}

	// Patch up the item list: make sure each item has a GUID.
	for idx := 0; idx < len(feed.Items); idx++ {
		itm := feed.Items[idx]
		if itm.GUID == "" {
			if itm.Link != "" {
				itm.GUID = itm.Link
			} else if itm.Title != "" {
				itm.GUID = itm.Title
			}
			feed.Items[idx] = itm
		}
	}

	// Work out which items are new, if any (based on the last updated TS we have)
	// If the TS is 0 then this is the first ever poll, so let's not send 10s of events
	// into the room and just do new ones from this point onwards.
	if s.Feeds[feedURL].NextPollTimestampSecs != 0 {
		items = s.newItems(feedURL, feed.Items)
	}

	now := time.Now().Unix() // Second resolution

	// Work out when to next poll this feed
	nextPollTsSec := now + minPollingIntervalSeconds
	if s.Feeds[feedURL].PollIntervalMins > int(minPollingIntervalSeconds/60) {
		nextPollTsSec = now + int64(s.Feeds[feedURL].PollIntervalMins*60)
	}
	// TODO: Handle the 'sy' Syndication extension to control update interval.
	// See http://www.feedforall.com/syndication.htm and http://web.resource.org/rss/1.0/modules/syndication/

	// map items to guid strings
	var guids []string
	for _, itm := range feed.Items {
		guids = append(guids, itm.GUID)
	}

	// Update the service config to persist the new times
	f := s.Feeds[feedURL]
	f.NextPollTimestampSecs = nextPollTsSec
	f.FeedUpdatedTimestampSecs = now
	f.RecentGUIDs = guids
	f.IsFailing = false
	s.Feeds[feedURL] = f

	return feed, items, nil
}

func (s *Service) newItems(feedURL string, allItems []*gofeed.Item) (items []gofeed.Item) {
	for _, i := range allItems {
		if i == nil {
			continue
		}
		// if we've seen this guid before, we've sent it before
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

		// Decode HTML for <title> and <description>:
		//   The RSS 2.0 Spec http://cyber.harvard.edu/rss/rss.html#hrelementsOfLtitemgt supports a bunch
		//   of weird ways to put HTML into <title> and <description> tags. Not all RSS feed producers run
		//   these fields through entity encoders (some have ' unencoded, others have it as &#8217;). We'll
		//   assume that all RSS fields are sending HTML for these fields and run them through a standard decoder.
		//   This will inevitably break for some people, but that group of people are probably smaller, so *shrug*.
		i.Title = html.UnescapeString(i.Title)
		i.Description = html.UnescapeString(i.Description)

		items = append(items, *i)
	}
	return
}

func (s *Service) sendToRooms(cli *matrix.Client, feedURL string, feed *gofeed.Feed, item gofeed.Item) error {
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

type userAgentRoundTripper struct {
	Transport http.RoundTripper
}

func (rt userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Go-NEB")
	return rt.Transport.RoundTrip(req)
}

func init() {
	lruCache := lrucache.New(1024*1024*20, 0) // 20 MB cache, no max-age
	cachingClient = &http.Client{
		Transport: userAgentRoundTripper{httpcache.NewTransport(lruCache)},
	}
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		r := &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
		return r
	})
	prometheus.MustRegister(pollCounter)
}
