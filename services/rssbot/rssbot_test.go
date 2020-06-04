package rssbot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/go-neb/types"
	"maunium.net/go/mautrix"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const rssFeedXML = `
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:wfw="http://wellformedweb.org/CommentAPI/"
	xmlns:dc="http://purl.org/dc/elements/1.1/"
	xmlns:atom="http://www.w3.org/2005/Atom"
	xmlns:sy="http://purl.org/rss/1.0/modules/syndication/"
	xmlns:slash="http://purl.org/rss/1.0/modules/slash/"
	>
<channel>
	<title>Mask Shop</title>
	<item>
		<title>New Item: Majora&#8217;s Mask</title>
		<link>http://go.neb/rss/majoras-mask</link>
		<author>The Skullkid!</author>
	</item>
</channel>
</rss>`

func createRSSClient(t *testing.T, feedURL string) *Service {
	database.SetServiceDB(&database.NopStorage{})
	// Replace the cachingClient with a mock so we can intercept RSS requests
	rssTrans := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != feedURL {
			return nil, errors.New("Unknown test URL")
		}
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(rssFeedXML)),
		}, nil
	})
	cachingClient = &http.Client{Transport: rssTrans}

	// Create the RSS service
	srv, err := types.CreateService("id", "rssbot", "@happy_mask_salesman:hyrule", []byte(
		`{"feeds": {"`+feedURL+`":{}}}`, // no config yet
	))

	if err != nil {
		t.Fatal(err)
	}

	rssbot := srv.(*Service)

	// Configure the service to force OnPoll to query the RSS feed and attempt to send results
	// to the right room.
	f := rssbot.Feeds[feedURL]
	f.Rooms = []id.RoomID{"!linksroom:hyrule"}
	f.NextPollTimestampSecs = time.Now().Unix()
	rssbot.Feeds[feedURL] = f

	return rssbot
}

func TestHTMLEntities(t *testing.T) {
	feedURL := "https://thehappymaskshop.hyrule"

	rssbot := createRSSClient(t, feedURL)

	// Create the Matrix client which will send the notification
	wg := sync.WaitGroup{}
	wg.Add(1)
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.Path, "/_matrix/client/r0/rooms/!linksroom:hyrule/send/m.room.message") {
			// Check content body to make sure it is decoded
			var msg mevt.MessageEventContent
			if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
				t.Fatal("Failed to decode request JSON: ", err)
				return nil, errors.New("Error handling matrix client test request")
			}
			want := "New Item: Majora\u2019s Mask" // 0x2019 = 8217
			if !strings.Contains(msg.Body, want) {
				t.Errorf("TestHTMLEntities: want '%s' in body, got '%s'", want, msg.Body)
			}

			wg.Done()
			return &http.Response{
				StatusCode: 200,
				Body: ioutil.NopCloser(bytes.NewBufferString(`
					{"event_id":"$123456:hyrule"}	
				`)),
			}, nil
		}
		return nil, errors.New("Unhandled matrix client test request")
	}
	matrixClient, _ := mautrix.NewClient("https://hyrule", "@happy_mask_salesman:hyrule", "its_a_secret")
	matrixClient.Client = &http.Client{Transport: matrixTrans}

	// Invoke OnPoll to trigger the RSS feed update
	_ = rssbot.OnPoll(matrixClient)

	// Check that the Matrix client sent a message
	wg.Wait()
}

func TestFeedItemFiltering(t *testing.T) {
	feedURL := "https://thehappymaskshop.hyrule"

	// Create rssbot client
	rssbot := createRSSClient(t, feedURL)

	feed := rssbot.Feeds[feedURL]
	feed.MustInclude.Title = []string{"Zelda"}
	rssbot.Feeds[feedURL] = feed

	_, items, _ := rssbot.queryFeed(feedURL)
	// Expect that we get no items if we filter for 'Zelda' in title
	if len(items) != 0 {
		t.Errorf("Expected 0 items, got %v", items)
	}

	// Recreate rssbot client
	rssbot = createRSSClient(t, feedURL)

	feed = rssbot.Feeds[feedURL]
	feed.MustInclude.Title = []string{"Majora"}
	rssbot.Feeds[feedURL] = feed

	_, items, _ = rssbot.queryFeed(feedURL)
	// Expect one item if we filter for 'Majora' in title
	if len(items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(items))
	}

	// Recreate rssbot client
	rssbot = createRSSClient(t, feedURL)

	feed = rssbot.Feeds[feedURL]
	feed.MustNotInclude.Author = []string{"kid"}
	rssbot.Feeds[feedURL] = feed

	_, items, _ = rssbot.queryFeed(feedURL)
	// 'kid' does not match an entire word in the author name, so it's not filtered
	if len(items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(items))
	}

	// Recreate rssbot client
	rssbot = createRSSClient(t, feedURL)

	feed = rssbot.Feeds[feedURL]
	feed.MustNotInclude.Author = []string{"Skullkid"}
	rssbot.Feeds[feedURL] = feed

	_, items, _ = rssbot.queryFeed(feedURL)
	// Expect no items if we filter for 'Skullkid' not in author name
	if len(items) != 0 {
		t.Errorf("Expected 0 items, got %v", items)
	}
}
