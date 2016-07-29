// Package matrix provides an HTTP client that can interact with a Homeserver via r0 APIs (/sync).
//
// It is NOT safe to access the field (or any sub-fields of) 'Rooms' concurrently. In essence, this
// structure MUST be treated as read-only. The matrix client will update this structure as new events
// arrive from the homeserver.
//
// Internally, the client has 1 goroutine for polling the server, and 1 goroutine for processing data
// returned. The polling goroutine communicates to the processing goroutine by a buffered channel
// which feedback loops if processing takes a while as it will delay more data from being pulled down
// if the buffer gets full. Modification of the 'Rooms' field of the client is done EXCLUSIVELY on the
// processing goroutine.
package matrix

import (
	"bytes"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"
)

var (
	filterJSON = json.RawMessage(`{"room":{"timeline":{"limit":0}}}`)
)

// Client represents a Matrix client.
type Client struct {
	HomeserverURL *url.URL
	Prefix        string
	UserID        string
	AccessToken   string
	Rooms         map[string]*Room
	Worker        *Worker
	syncingMutex  sync.Mutex
	syncingID     uint32 // Identifies the current Sync. Only one Sync can be active at any given time.
	httpClient    *http.Client
	filterID      string
}

func (cli *Client) buildURL(urlPath ...string) string {
	// copy the URL. Purposefully ignore error as the input is from a valid URL already
	hsURL, _ := url.Parse(cli.HomeserverURL.String())
	parts := []string{hsURL.Path, cli.Prefix}
	parts = append(parts, urlPath...)
	hsURL.Path = path.Join(parts...)
	query := hsURL.Query()
	query.Set("access_token", cli.AccessToken)
	hsURL.RawQuery = query.Encode()
	return hsURL.String()
}

func (cli *Client) buildURLWithQuery(urlPath []string, urlQuery map[string]string) string {
	u, _ := url.Parse(cli.buildURL(urlPath...))
	q := u.Query()
	for k, v := range urlQuery {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// JoinRoom joins the client to a room ID or alias. Returns a room ID.
func (cli *Client) JoinRoom(roomIDorAlias, serverName string) (string, error) {
	var urlPath string
	if serverName != "" {
		urlPath = cli.buildURLWithQuery([]string{"join", roomIDorAlias}, map[string]string{
			"server_name": serverName,
		})
	} else {
		urlPath = cli.buildURL("join", roomIDorAlias)
	}

	resBytes, err := cli.sendJSON("POST", urlPath, `{}`)
	if err != nil {
		return "", err
	}
	var joinRoomResponse joinRoomHTTPResponse
	if err = json.Unmarshal(resBytes, &joinRoomResponse); err != nil {
		return "", err
	}
	return joinRoomResponse.RoomID, nil
}

// SendMessageEvent sends a message event into a room, returning the event_id on success.
// contentJSON should be a pointer to something that can be encoded as JSON using json.Marshal.
func (cli *Client) SendMessageEvent(roomID string, eventType string, contentJSON interface{}) (string, error) {
	txnID := "go" + strconv.FormatInt(time.Now().UnixNano(), 10)
	urlPath := cli.buildURL("rooms", roomID, "send", eventType, txnID)
	resBytes, err := cli.sendJSON("PUT", urlPath, contentJSON)
	if err != nil {
		return "", err
	}
	var sendEventResponse sendEventHTTPResponse
	if err = json.Unmarshal(resBytes, &sendEventResponse); err != nil {
		return "", err
	}
	return sendEventResponse.EventID, nil
}

// SendText sends an m.room.message event into the given room with a msgtype of m.text
func (cli *Client) SendText(roomID, text string) (string, error) {
	return cli.SendMessageEvent(roomID, "m.room.message",
		TextMessage{"m.text", text})
}

// Sync starts syncing with the provided Homeserver. This function will be invoked continually.
// If Sync is called twice then the first sync will be stopped.
func (cli *Client) Sync() {
	// Mark the client as syncing.
	// We will keep syncing until the syncing state changes. Either because
	// Sync is called or StopSync is called.
	syncingID := cli.incrementSyncingID()
	logger := log.WithFields(log.Fields{
		"syncing": syncingID,
		"user_id": cli.UserID,
	})

	// TODO: Store the filter ID and sync token in the database
	filterID, err := cli.createFilter()
	if err != nil {
		logger.WithError(err).Fatal("Failed to create filter")
		// TODO: Maybe do some sort of error handling here?
	}
	cli.filterID = filterID
	logger.WithField("filter", filterID).Print("Got filter ID")
	nextToken := ""

	logger.Print("Starting sync")

	channel := make(chan syncHTTPResponse, 5)

	go func() {
		for response := range channel {
			cli.Worker.onSyncHTTPResponse(response)
		}
	}()
	defer close(channel)

	for {
		// Do a /sync
		syncBytes, err := cli.doSync(30000, nextToken)
		if err != nil {
			logger.WithError(err).Warn("doSync failed")
			time.Sleep(5 * time.Second)
			continue
		}

		// Decode sync response into syncHTTPResponse
		var syncResponse syncHTTPResponse
		if err = json.Unmarshal(syncBytes, &syncResponse); err != nil {
			logger.WithError(err).Warn("Failed to decode sync data")
			time.Sleep(5 * time.Second)
			continue
		}

		//  Check that the syncing state hasn't changed
		// Either because we've stopped syncing or another sync has been started.
		// We discard the response from our sync.
		// TODO: Store the next_batch token so that the next sync can resume
		// from where this sync left off.
		if cli.getSyncingID() != syncingID {
			logger.Print("Stopping sync")
			return
		}

		// Update client state
		nextToken = syncResponse.NextBatch
		logger.WithField("next_batch", nextToken).Print("Received sync response")
		channel <- syncResponse
	}
}

func (cli *Client) incrementSyncingID() uint32 {
	cli.syncingMutex.Lock()
	defer cli.syncingMutex.Unlock()
	cli.syncingID++
	return cli.syncingID
}

func (cli *Client) getSyncingID() uint32 {
	cli.syncingMutex.Lock()
	defer cli.syncingMutex.Unlock()
	return cli.syncingID
}

// StopSync stops the ongoing sync started by Sync.
func (cli *Client) StopSync() {
	// Advance the syncing state so that any running Syncs will terminate.
	cli.incrementSyncingID()
}

// This should only be called by the worker goroutine
func (cli *Client) getOrCreateRoom(roomID string) *Room {
	room := cli.Rooms[roomID]
	if room == nil { // create a new Room
		room = NewRoom(roomID)
		cli.Rooms[roomID] = room
	}
	return room
}

func (cli *Client) sendJSON(method string, httpURL string, contentJSON interface{}) ([]byte, error) {
	jsonStr, err := json.Marshal(contentJSON)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, httpURL, bytes.NewBuffer(jsonStr))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	logger := log.WithFields(log.Fields{
		"method": method,
		"url":    httpURL,
		"json":   string(jsonStr),
	})
	logger.Print("Sending JSON request")
	res, err := cli.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Warn("Failed to send JSON request")
		return nil, err
	}
	defer res.Body.Close()
	contents, err := ioutil.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		logger.WithFields(log.Fields{
			"code": res.StatusCode,
			"body": string(contents),
		}).Warn("Failed to send JSON request")
		return nil, errors.HTTPError{
			Code:    res.StatusCode,
			Message: "Failed to " + method + " JSON: HTTP " + strconv.Itoa(res.StatusCode),
		}
	}
	if err != nil {
		logger.WithError(err).Warn("Failed to read response")
		return nil, err
	}
	return contents, nil
}

func (cli *Client) createFilter() (string, error) {
	urlPath := cli.buildURL("user", cli.UserID, "filter")
	resBytes, err := cli.sendJSON("POST", urlPath, &filterJSON)
	if err != nil {
		return "", err
	}
	var filterResponse filterHTTPResponse
	if err = json.Unmarshal(resBytes, &filterResponse); err != nil {
		return "", err
	}
	return filterResponse.FilterID, nil
}

func (cli *Client) doSync(timeout int, since string) ([]byte, error) {
	query := map[string]string{
		"timeout": strconv.Itoa(timeout),
	}
	if since != "" {
		query["since"] = since
	}
	if cli.filterID != "" {
		query["filter"] = cli.filterID
	}
	urlPath := cli.buildURLWithQuery([]string{"sync"}, query)
	log.WithFields(log.Fields{
		"since":   since,
		"timeout": timeout,
	}).Print("Syncing")
	res, err := http.Get(urlPath)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	contents, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return contents, nil
}

// NewClient creates a new Matrix Client ready for syncing
func NewClient(homeserverURL *url.URL, accessToken string, userID string) *Client {
	cli := Client{
		AccessToken:   accessToken,
		HomeserverURL: homeserverURL,
		UserID:        userID,
		Prefix:        "/_matrix/client/r0",
	}
	cli.Worker = newWorker(&cli)
	cli.Rooms = make(map[string]*Room)
	cli.httpClient = &http.Client{}

	return &cli
}
