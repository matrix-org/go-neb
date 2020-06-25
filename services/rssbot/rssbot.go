// Package rssbot implements a Service capable of reading Atom/RSS feeds.
package rssbot

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/die-net/lrucache"
	"github.com/gregjones/httpcache"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/mmcdole/gofeed"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the RSS Bot service
const ServiceType = "rssbot"

var cachingClient *http.Client

var (
	pollCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_rss_polls_total",
		Help: "The number of feed polls from RSS services",
	}, []string{"http_status"})
)

const minPollingIntervalSeconds = 60 * 5 // 5 min (News feeds can be genuinely spammy)

// includeRules contains the rules for including or excluding a feed item. For the fields Author, Title
// and Description in a feed item, there can be some words specified in the config that determine whether
// the item will be displayed or not, depending on whether these words are included in that field.
//
//   - If specified in the `must_include` field, the feed item must include at least one word for each field
//     that has been specified. This means that if some words have been specified for both Author and Title,
//     both the Author and Title must contain at least one of their respective words or the item will be skipped.
//   - If specified in the `must_not_include` field, the feed item fields must not contain any of the words
//     that were specified for each field. This means that if some words have been specified for both Author
//     and Title, if either of them includes at least one of their respective words, the item will be skipped,
//     even in the case that the item matched the `must_include` rules.
//
//   In both cases, specifying an empty list for a field or not specifying anything causes the field to be ignored.
//   The field being checked each time will be split into words (any non-alphanumeric character starts a new word)
//   and they will be checked against the provided list.
type includeRules struct {
	// Author is a case-sensitive list of words that the author name must contain or not contain.
	Author []string `json:"author"`
	// Title is a case-sensitive list of words that the author name must contain or not contain.
	Title []string `json:"title"`
	// Description is a case-sensitive list of words that the author name must contain or not contain.
	Description []string `json:"description"`
}

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
		Rooms []id.RoomID `json:"rooms"`
		// True if rss bot is unable to poll this feed. This is populated by Go-NEB. Use /getService to
		// retrieve this value.
		IsFailing bool `json:"is_failing"`
		// The time of the last successful poll. This is populated by Go-NEB. Use /getService to retrieve
		// this value.
		FeedUpdatedTimestampSecs int64 `json:"last_updated_ts_secs"`
		// Specified fields must each include at least one of these words.
		MustInclude includeRules `json:"must_include"`
		// None of the specified fields must include any of these words.
		MustNotInclude includeRules `json:"must_not_include"`
		// Internal field. When we should poll again.
		NextPollTimestampSecs int64
		// Internal field. The most recently seen GUIDs. Sized to the number of items in the feed.
		RecentGUIDs []string
	} `json:"feeds"`
}

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *Service) Register(oldService types.Service, client types.MatrixClient) error {
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
			return errors.New("An RSS feed must be specified")
		}
		return nil
	}
	// Make sure we can parse the feed
	for feedURL, feedInfo := range s.Feeds {
		if _, err := readFeed(feedURL); err != nil {
			return fmt.Errorf("Failed to read URL %s: %s", feedURL, err.Error())
		}
		if len(feedInfo.Rooms) == 0 {
			return fmt.Errorf("Feed %s has no rooms to send updates to", feedURL)
		}
	}

	s.joinRooms(client)
	return nil
}

func (s *Service) joinRooms(client types.MatrixClient) {
	roomSet := make(map[id.RoomID]bool)
	for _, feedInfo := range s.Feeds {
		for _, roomID := range feedInfo.Rooms {
			roomSet[roomID] = true
		}
	}

	for roomID := range roomSet {
		if _, err := client.JoinRoom(roomID.String(), "", nil); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    roomID,
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
func (s *Service) OnPoll(cli types.MatrixClient) time.Time {
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
		logger.WithFields(log.Fields{
			"feed_url":   u,
			"feed_items": len(feed.Items),
			"new_items":  len(items),
		}).Info("Sending new items")
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
	if err != nil {
		herr, ok := err.(gofeed.HTTPError)
		statusCode := 0 // e.g. network timeout
		if ok {
			statusCode = herr.StatusCode
		}
		pollCounter.With(prometheus.Labels{"http_status": strconv.Itoa(statusCode)}).Inc()
	} else {
		pollCounter.With(prometheus.Labels{"http_status": "200"}).Inc() // technically 2xx but gofeed doesn't tell us which
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
	feed, err := readFeed(feedURL)
	// check for no items in addition to any returned errors as it appears some RSS feeds
	// do not consistently return items.
	if err == nil && len(feed.Items) == 0 {
		err = errors.New("feed has 0 items")
	}

	if err != nil {
		f := s.Feeds[feedURL]
		f.IsFailing = true
		s.Feeds[feedURL] = f
		return nil, items, err
	}

	// Patch up the item list: make sure each item has a GUID.
	ensureItemsHaveGUIDs(feed)

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

	// Work out which GUIDs to remember. We don't want to remember every GUID ever as that leads to completely
	// unbounded growth of data.
	f := s.Feeds[feedURL]
	// Some RSS feeds can return a very small number of items then bounce
	// back to their "normal" size, so we cannot just clobber the recent GUID list per request or else we'll
	// forget what we sent and resend it. Instead, we'll keep 2x the max number of items that we've ever
	// seen from this feed, up to a max of 10,000.
	maxGuids := 2 * len(feed.Items)
	if len(f.RecentGUIDs) > maxGuids {
		maxGuids = len(f.RecentGUIDs) // already 2x'd.
	}
	if maxGuids > 10000 {
		maxGuids = 10000
	}

	lastSet := uniqueStrings(f.RecentGUIDs) // e.g. [4,5,6]
	thisSet := uniqueGuids(feed.Items)      // e.g. [1,2,3]
	guids := append(thisSet, lastSet...)    // e.g. [1,2,3,4,5,6]
	guids = uniqueStrings(guids)
	if len(guids) > maxGuids {
		// Critically this favours the NEWEST elements, which are the ones we're most likely to see again.
		guids = guids[0:maxGuids]
	}

	// Update the service config to persist the new times
	f.NextPollTimestampSecs = nextPollTsSec
	f.FeedUpdatedTimestampSecs = now
	f.RecentGUIDs = guids
	f.IsFailing = false
	s.Feeds[feedURL] = f

	return feed, items, nil
}

// containsAny takes a string and an array of words and returns whether any of the words
// in the list are contained in the string. The words in the string are considered to be
// separated by any non-alphanumeric character.
func containsAny(item string, filterWords []string) bool {
	itemWords := strings.FieldsFunc(item, func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	})
	for _, itemWord := range itemWords {
		for _, filterWord := range filterWords {
			if filterWord == itemWord {
				return true
			}
		}
	}
	return false
}

func itemFiltered(i *gofeed.Item, mustInclude, mustNotInclude *includeRules) bool {
	// At least one word for each field that has been specified must be included for an item to pass the filter.
	if (i.Author != nil && len(mustInclude.Author) > 0 && !containsAny(i.Author.Name, mustInclude.Author)) ||
		(len(mustInclude.Title) > 0 && !containsAny(i.Title, mustInclude.Title)) ||
		(len(mustInclude.Description) > 0 && !containsAny(i.Description, mustInclude.Description)) {
		return true
	}

	// If at least one word of any field that has been specified is included in the item, it doesn't pass the filter.
	if (i.Author != nil && containsAny(i.Author.Name, mustNotInclude.Author)) ||
		containsAny(i.Title, mustNotInclude.Title) ||
		containsAny(i.Description, mustNotInclude.Description) {
		return true
	}
	return false
}

func (s *Service) newItems(feedURL string, allItems []*gofeed.Item) (items []gofeed.Item) {
	mustInclude := s.Feeds[feedURL].MustInclude
	mustNotInclude := s.Feeds[feedURL].MustNotInclude

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
		if i.Author != nil {
			i.Author.Name = html.UnescapeString(i.Author.Name)
			i.Author.Email = html.UnescapeString(i.Author.Email)
		}

		if !itemFiltered(i, &mustInclude, &mustNotInclude) {
			items = append(items, *i)
		}
	}
	return
}

func (s *Service) sendToRooms(cli types.MatrixClient, feedURL string, feed *gofeed.Feed, item gofeed.Item) error {
	logger := log.WithFields(log.Fields{
		"feed_url": feedURL,
		"title":    item.Title,
		"guid":     item.GUID,
	})
	logger.Info("Sending new feed item")
	for _, roomID := range s.Feeds[feedURL].Rooms {
		if _, err := cli.SendMessageEvent(roomID, mevt.EventMessage, itemToHTML(feed, item)); err != nil {
			logger.WithError(err).WithField("room_id", roomID).Error("Failed to send to room")
		}
	}
	return nil
}

func itemToHTML(feed *gofeed.Feed, item gofeed.Item) mevt.MessageEventContent {
	// If an item does not have a title, try using the feed's title instead
	// Create a new variable instead of mutating that which is passed in
	itemTitle := item.Title
	if itemTitle == "" {
		itemTitle = feed.Title
	}

	fmtBody := fmt.Sprintf("<strong>%s</strong>:<br><a href=\"%s\"><strong>%s</strong></a>",
		html.EscapeString(feed.Title), html.EscapeString(item.Link), html.EscapeString(itemTitle))
	if item.Author != nil {
		if len(item.Author.Name) > 0 && len(item.Author.Email) > 0 {
			fmtBody += fmt.Sprintf(" by <a href=\"mailto:%s\">%s</a>", html.EscapeString(item.Author.Email),
				html.EscapeString(item.Author.Name))
		} else if len(item.Author.Name) > 0 {
			fmtBody += fmt.Sprintf(" by %s", html.EscapeString(item.Author.Name))
		} else if len(item.Author.Email) > 0 {
			fmtBody += fmt.Sprintf(" by <a href=\"mailto:%s\">%s</a>", html.EscapeString(item.Author.Email),
				html.EscapeString(item.Author.Email))
		}
	}
	return mevt.MessageEventContent{
		Body: fmt.Sprintf("%s: %s ( %s )",
			html.EscapeString(feed.Title), html.EscapeString(itemTitle), html.EscapeString(item.Link)),
		MsgType:       "m.notice",
		Format:        mevt.FormatHTML,
		FormattedBody: fmtBody,
		// <strong>FeedTitle</strong>:
		// <br>
		// <a href="url-of-the-entry"><strong>Title of the Entry</strong></a>
	}
}

func ensureItemsHaveGUIDs(feed *gofeed.Feed) {
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
}

// uniqueStrings returns a new slice of strings with duplicate elements removed.
// Order is otherwise preserved.
func uniqueStrings(a []string) []string {
	ret := []string{}
	seen := make(map[string]bool)
	for _, str := range a {
		if seen[str] {
			continue
		}
		seen[str] = true
		ret = append(ret, str)
	}
	return ret
}

// uniqueGuids returns a new slice of GUID strings with duplicate elements removed.
// Order is otherwise preserved.
func uniqueGuids(a []*gofeed.Item) []string {
	ret := []string{}
	seen := make(map[string]bool)
	for _, item := range a {
		if seen[item.GUID] {
			continue
		}
		seen[item.GUID] = true
		ret = append(ret, item.GUID)
	}
	return ret
}

type userAgentRoundTripper struct {
	Transport http.RoundTripper
}

func (rt userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Go-NEB")
	return rt.Transport.RoundTrip(req)
}

func readFeed(feedURL string) (*gofeed.Feed, error) {
	// Don't use fp.ParseURL because it leaks on non-2xx responses as of 2016/11/29 (cac19c6c27)
	fp := gofeed.NewParser()
	resp, err := cachingClient.Get(feedURL)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gofeed.HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}
	return fp.Parse(resp.Body)
}

func init() {
	lruCache := lrucache.New(1024*1024*20, 0) // 20 MB cache, no max-age
	cachingClient = &http.Client{
		Transport: userAgentRoundTripper{httpcache.NewTransport(lruCache)},
	}
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		r := &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
		return r
	})
	prometheus.MustRegister(pollCounter)
}
