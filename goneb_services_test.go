package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigureClient(t *testing.T) {
	mux := http.NewServeMux()
	mxTripper := newMatrixTripper()
	setup(envVars{
		BaseURL:      "http://go.neb",
		DatabaseType: "sqlite3",
		DatabaseURL:  ":memory:",
	}, mux, &http.Client{
		Transport: mxTripper,
	})

	mxTripper.ClearHandlers()
	mockWriter := httptest.NewRecorder()
	syncChan := make(chan string)
	mxTripper.HandlePOSTFilter("@link:hyrule")
	mxTripper.Handle("GET", "/_matrix/client/r0/sync",
		func(req *http.Request) (*http.Response, error) {
			syncChan <- "sync"
			return newResponse(200, `{
				"next_batch":"11_22_33_44",
				"rooms": {}
			}`), nil
		},
	)

	mockReq, _ := http.NewRequest("POST", "http://go.neb/admin/configureClient", bytes.NewBufferString(`
	{
		"UserID":"@link:hyrule",
		"HomeserverURL":"http://hyrule.loz",
		"AccessToken":"dangeroustogoalone",
		"Sync":true,
		"AutoJoinRooms":true
	}`))
	mux.ServeHTTP(mockWriter, mockReq)
	expectCode := 200
	if mockWriter.Code != expectCode {
		t.Errorf("TestConfigureClient wanted HTTP status %d, got %d", expectCode, mockWriter.Code)
	}

	<-syncChan
}

func TestRespondToEcho(t *testing.T) {
	mux := http.NewServeMux()
	mxTripper := newMatrixTripper()
	setup(envVars{
		BaseURL:      "http://go.neb",
		DatabaseType: "sqlite3",
		DatabaseURL:  ":memory:",
	}, mux, &http.Client{
		Transport: mxTripper,
	})

	mxTripper.ClearHandlers()
	mockWriter := httptest.NewRecorder()
	reqChan := make(chan string)
	mxTripper.HandlePOSTFilter("@link:hyrule")
	mxTripper.Handle("GET", "/_matrix/client/r0/sync",
		func(req *http.Request) (*http.Response, error) {
			reqBody := <-reqChan
			return newResponse(200, reqBody), nil
		},
	)
	mxTripper.Handle("POST", "/_matrix/client/r0/keys/upload", func(req *http.Request) (*http.Response, error) {
		return newResponse(200, `{}`), nil
	})

	var joinedRoom string
	var joinedRoomBody []byte
	mxTripper.Handle("POST", "/_matrix/client/r0/join/*", func(req *http.Request) (*http.Response, error) {
		parts := strings.Split(req.URL.String(), "/")
		joinedRoom = parts[len(parts)-1]
		n, _ := req.Body.Read(joinedRoomBody)
		joinedRoomBody = joinedRoomBody[:n]
		return newResponse(200, `{}`), nil
	})

	var roomMsgBody []byte
	mxTripper.Handle("PUT", "/_matrix/client/r0/rooms/!greatdekutree:hyrule/send/m.room.message/*", func(req *http.Request) (*http.Response, error) {
		n, _ := req.Body.Read(roomMsgBody)
		roomMsgBody = roomMsgBody[:n]
		return newResponse(200, `{}`), nil
	})

	// configure the client
	clientConfigReq, _ := http.NewRequest("POST", "http://go.neb/admin/configureClient", bytes.NewBufferString(`
	{
		"UserID":"@link:hyrule",
		"HomeserverURL":"http://hyrule.loz",
		"AccessToken":"dangeroustogoalone",
		"Sync":true,
		"AutoJoinRooms":true
	}`))
	mux.ServeHTTP(mockWriter, clientConfigReq)

	// configure the echo service
	serviceConfigReq, _ := http.NewRequest("POST", "http://go.neb/admin/configureService", bytes.NewBufferString(`
	{
		"Type": "echo",
		"Id": "test_echo_service",
		"UserID": "@link:hyrule",
		"Config": {}
	}`))
	mux.ServeHTTP(mockWriter, serviceConfigReq)

	// get the initial syncs out of the way
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`

	// send neb an invite to a room
	reqChan <- `{
		"next_batch":"11_22_33_44",
		"rooms": {
			"invite": {
				"!greatdekutree:hyrule": {"invite_state": {"events": [{
					"type": "m.room.member",
					"sender": "@navi:hyrule",
					"content": {"membership": "invite"},
					"state_key": "@link:hyrule",
					"origin_server_ts": 10000,
					"unsigned": {"age": 100},
					"event_id": "evt123"
				}]}}}
		}
	}`

	// wait for it to be processed
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`

	expectedRoom := "%21greatdekutree:hyrule"
	if joinedRoom != expectedRoom {
		t.Errorf("Expected join for room %v, got %v", expectedRoom, joinedRoom)
	}
	if expectedBody := `{"inviter":"@navi:hyrule"}`; string(joinedRoomBody) != expectedBody {
		t.Errorf("Expected join message body to be %v, got %v", expectedBody, string(joinedRoomBody))
	}

	// send neb an !echo message
	reqChan <- `{
		"next_batch":"11_22_33_44",
		"rooms": {
			"join": {
				"!greatdekutree:hyrule": {"timeline": {"events": [{
					"type": "m.room.message",
					"sender": "@navi:hyrule",
					"content": {"body": "!echo save zelda", "msgtype": "m.text"},
					"origin_server_ts": 10000,
					"unsigned": {"age": 100},
					"event_id": "evt124"
				}]}}}
		}
	}`

	// wait for it to be processed
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`

	if expectedEchoResp := `{"msgtype":"m.notice","body":"save zelda"}`; string(roomMsgBody) != expectedEchoResp {
		t.Errorf("Expected echo response to be `%v`, got `%v`", expectedEchoResp, string(roomMsgBody))
	}
}
