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
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"
)

var (
	filterJSON = json.RawMessage(`{"room":{"timeline":{"limit":50}}}`)
)

// NextBatchStorer controls loading/saving of next_batch tokens for users
type NextBatchStorer interface {
	// Save a next_batch token for a given user. Best effort.
	Save(userID, nextBatch string)
	// Load a next_batch token for a given user. Return an empty string if no token exists.
	Load(userID string) string
}

// noopNextBatchStore does not load or save next_batch tokens.
type noopNextBatchStore struct{}

func (s noopNextBatchStore) Save(userID, nextBatch string) {}
func (s noopNextBatchStore) Load(userID string) string     { return "" }

// Client represents a Matrix client.
type Client struct {
	HomeserverURL   *url.URL
	Prefix          string
	UserID          string
	AccessToken     string
	Rooms           map[string]*Room
	Worker          *Worker
	syncingMutex    sync.Mutex
	syncingID       uint32 // Identifies the current Sync. Only one Sync can be active at any given time.
	httpClient      *http.Client
	filterID        string
	NextBatchStorer NextBatchStorer
}

func (cli *Client) buildURL(urlPath ...string) string {
	ps := []string{cli.Prefix}
	for _, p := range urlPath {
		ps = append(ps, p)
	}
	return cli.buildBaseURL(ps...)
}

func (cli *Client) buildBaseURL(urlPath ...string) string {
	// copy the URL. Purposefully ignore error as the input is from a valid URL already
	hsURL, _ := url.Parse(cli.HomeserverURL.String())
	parts := []string{hsURL.Path}
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

// JoinRoom joins the client to a room ID or alias. If serverName is specified, this will be added as a query param
// to instruct the homeserver to join via that server. If invitingUserID is specified, the inviting user ID will be
// inserted into the content of the join request. Returns a room ID.
func (cli *Client) JoinRoom(roomIDorAlias, serverName, invitingUserID string) (string, error) {
	var urlPath string
	if serverName != "" {
		urlPath = cli.buildURLWithQuery([]string{"join", roomIDorAlias}, map[string]string{
			"server_name": serverName,
		})
	} else {
		urlPath = cli.buildURL("join", roomIDorAlias)
	}

	content := struct {
		Inviter string `json:"inviter,omitempty"`
	}{}
	content.Inviter = invitingUserID

	resBytes, err := cli.sendJSON("POST", urlPath, content)
	if err != nil {
		return "", err
	}
	var joinRoomResponse joinRoomHTTPResponse
	if err = json.Unmarshal(resBytes, &joinRoomResponse); err != nil {
		return "", err
	}
	return joinRoomResponse.RoomID, nil
}

// SetDisplayName sets the user's profile display name
func (cli *Client) SetDisplayName(displayName string) error {
	urlPath := cli.buildURL("profile", cli.UserID, "displayname")
	s := struct {
		DisplayName string `json:"displayname"`
	}{displayName}
	_, err := cli.sendJSON("PUT", urlPath, &s)
	return err
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

// UploadLink uploads an HTTP URL and then returns an MXC URI.
func (cli *Client) UploadLink(link string) (string, error) {
	res, err := http.Get(link)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return "", err
	}
	return cli.UploadToContentRepo(res.Body, res.Header.Get("Content-Type"), res.ContentLength)
}

// UploadToContentRepo uploads the given bytes to the content repository and returns an MXC URI.
func (cli *Client) UploadToContentRepo(content io.Reader, contentType string, contentLength int64) (string, error) {
	req, err := http.NewRequest("POST", cli.buildBaseURL("_matrix/media/r0/upload"), content)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = contentLength
	log.WithFields(log.Fields{
		"content_type":   contentType,
		"content_length": contentLength,
	}).Print("Uploading to content repo")
	res, err := cli.httpClient.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Upload request returned HTTP %d", res.StatusCode)
	}
	m := struct {
		ContentURI string `json:"content_uri"`
	}{}
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return "", err
	}
	return m.ContentURI, nil
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

	// TODO: Store the filter ID in the database
	filterID, err := cli.createFilter()
	if err != nil {
		logger.WithError(err).Fatal("Failed to create filter")
		// TODO: Maybe do some sort of error handling here?
	}
	cli.filterID = filterID
	logger.WithField("filter", filterID).Print("Got filter ID")
	nextToken := cli.NextBatchStorer.Load(cli.UserID)

	logger.WithField("next_batch", nextToken).Print("Starting sync")

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
		if cli.getSyncingID() != syncingID {
			logger.Print("Stopping sync")
			return
		}

		processResponse := cli.shouldProcessResponse(nextToken, &syncResponse)
		nextToken = syncResponse.NextBatch
		logger.WithField("next_batch", nextToken).Print("Received sync response")

		// Save the token now *before* passing it through to the worker. This means it's possible
		// to not process some events, but it means that we won't get constantly stuck processing
		// a malformed/buggy event which keeps making us panic.
		cli.NextBatchStorer.Save(cli.UserID, nextToken)

		if processResponse {
			// Update client state
			channel <- syncResponse
		}
	}
}

// shouldProcessResponse returns true if the response should be processed. May modify the response to remove
// stuff that shouldn't be processed.
func (cli *Client) shouldProcessResponse(tokenOnSync string, syncResponse *syncHTTPResponse) bool {
	if tokenOnSync == "" {
		return false
	}
	// This is a horrible hack because /sync will return the most recent messages for a room
	// as soon as you /join it. We do NOT want to process those events in that particular room
	// because they may have already been processed (if you toggle the bot in/out of the room).
	//
	// Work around this by inspecting each room's timeline and seeing if an m.room.member event for us
	// exists and is "join" and then discard processing that room entirely if so.
	// TODO: We probably want to process the !commands from after the last join event in the timeline.
	for roomID, roomData := range syncResponse.Rooms.Join {
		for i := len(roomData.Timeline.Events) - 1; i >= 0; i-- {
			e := roomData.Timeline.Events[i]
			if e.Type == "m.room.member" && e.StateKey == cli.UserID {
				m := e.Content["membership"]
				mship, ok := m.(string)
				if !ok {
					continue
				}
				if mship == "join" {
					log.WithFields(log.Fields{
						"room_id":     roomID,
						"user_id":     cli.UserID,
						"start_token": tokenOnSync,
					}).Info("Discarding /sync events in room: just joined it.")
					_, ok := syncResponse.Rooms.Join[roomID]
					if !ok {
						panic("room " + roomID + " does not exist in Join?!")
					}
					delete(syncResponse.Rooms.Join, roomID)   // don't re-process !commands
					delete(syncResponse.Rooms.Invite, roomID) // don't re-process invites
					break
				}
			}
		}
	}
	return true
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
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		logger.WithError(err).Warn("Failed to send JSON request")
		return nil, err
	}
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
		"user_id": cli.UserID,
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
	// By default, use a no-op next_batch storer which will never save tokens and always
	// "load" the empty string as a token. The client will work with this storer: it just won't
	// remember the token across restarts. In practice, a database backend should be used.
	cli.NextBatchStorer = noopNextBatchStore{}
	cli.Rooms = make(map[string]*Room)
	cli.httpClient = &http.Client{}

	return &cli
}
