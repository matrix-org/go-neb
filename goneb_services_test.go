package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/olm"
	mevt "maunium.net/go/mautrix/event"
)

func setupMockServer() (*http.ServeMux, *matrixTripper, *httptest.ResponseRecorder, chan string) {
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
			if _, ok := req.URL.Query()["since"]; !ok {
				return newResponse(200, `{"next_batch":"11_22_33_44", "rooms": {}}`), nil
			}
			reqBody := <-reqChan
			return newResponse(200, reqBody), nil
		},
	)
	return mux, mxTripper, mockWriter, reqChan
}

func TestConfigureClient(t *testing.T) {
	mux, _, mockWriter, _ := setupMockServer()

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
}

func TestRespondToEcho(t *testing.T) {
	mux, mxTripper, mockWriter, reqChan := setupMockServer()

	mxTripper.Handle("POST", "/_matrix/client/r0/keys/upload", func(req *http.Request) (*http.Response, error) {
		return newResponse(200, `{}`), nil
	})

	var joinedRoom string
	var joinedRoomBody []byte
	mxTripper.Handle("POST", "/_matrix/client/r0/join/*", func(req *http.Request) (*http.Response, error) {
		parts := strings.Split(req.URL.String(), "/")
		joinedRoom = parts[len(parts)-1]
		joinedRoomBody, _ = ioutil.ReadAll(req.Body)
		return newResponse(200, `{}`), nil
	})

	var roomMsgBody []byte
	mxTripper.Handle("PUT", "/_matrix/client/r0/rooms/!greatdekutree:hyrule/send/m.room.message/*", func(req *http.Request) (*http.Response, error) {
		roomMsgBody, _ = ioutil.ReadAll(req.Body)
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

func TestEncryptedRespondToEcho(t *testing.T) {
	mux, mxTripper, mockWriter, reqChan := setupMockServer()

	// create the two accounts, inbound and outbound sessions, both the bot and mock ones
	accountMock := olm.NewAccount()
	accountBot := olm.NewAccount()
	signingKeyMock, identityKeyMock := accountMock.IdentityKeys()
	signingKeyBot, identityKeyBot := accountBot.IdentityKeys()
	ogsBot := crypto.NewOutboundGroupSession("!greatdekutree:hyrule")
	ogsBot.Shared = true
	igsMock, err := crypto.NewInboundGroupSession(identityKeyBot, signingKeyBot, "!greatdekutree:hyrule", ogsBot.Internal.Key())
	if err != nil {
		t.Errorf("Error creating mock IGS: %v", err)
	}
	ogsMock := crypto.NewOutboundGroupSession("!greatdekutree:hyrule")
	ogsMock.Shared = true
	igsBot, err := crypto.NewInboundGroupSession(identityKeyMock, signingKeyMock, "!greatdekutree:hyrule", ogsMock.Internal.Key())
	if err != nil {
		t.Errorf("Error creating bot IGS: %v", err)
	}

	mxTripper.Handle("POST", "/_matrix/client/r0/keys/upload", func(req *http.Request) (*http.Response, error) {
		return newResponse(200, `{}`), nil
	})

	var joinedRoom string
	var joinedRoomBody []byte
	mxTripper.Handle("POST", "/_matrix/client/r0/join/*", func(req *http.Request) (*http.Response, error) {
		parts := strings.Split(req.URL.String(), "/")
		joinedRoom = parts[len(parts)-1]
		joinedRoomBody, _ = ioutil.ReadAll(req.Body)
		return newResponse(200, `{}`), nil
	})

	var decryptedMsg string
	mxTripper.Handle("PUT", "/_matrix/client/r0/rooms/!greatdekutree:hyrule/send/m.room.encrypted/*", func(req *http.Request) (*http.Response, error) {
		encryptedMsg, _ := ioutil.ReadAll(req.Body)
		var encryptedContent mevt.EncryptedEventContent
		encryptedContent.UnmarshalJSON(encryptedMsg)
		decryptedMsgBytes, _, err := igsMock.Internal.Decrypt(encryptedContent.MegolmCiphertext)
		if err != nil {
			t.Errorf("Error decrypting message sent by bot: %v", err)
		}
		decryptedMsg = string(decryptedMsgBytes)
		return newResponse(200, `{}`), nil
	})

	// configure the client
	clientConfigReq, _ := http.NewRequest("POST", "http://go.neb/admin/configureClient", bytes.NewBufferString(`
	{
		"UserID":"@link:hyrule",
		"DeviceID":"mastersword",
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

	// send neb the room state: encrypted with one member
	reqChan <- `{
		"next_batch":"11_22_33_44",
		"rooms": {
			"join": {"!greatdekutree:hyrule": {"timeline": {"events": [{
					"type": "m.room.encryption",
					"state_key": "",
					"content": {
						"algorithm": "m.megolm.v1.aes-sha2"
					}
				}, {
					"type": "m.room.member",
					"sender": "@navi:hyrule",
					"content": {"membership": "join", "displayname": "Navi"},
					"state_key": "@navi:hyrule",
					"origin_server_ts": 100,
					"event_id": "evt124"
				}]}}}
		}
	}`

	// wait for it to be processed
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`

	// DB is initialized, store the megolm sessions from before for the bot to be able to decrypt and encrypt
	sqlDB, dialect := database.GetServiceDB().(*database.ServiceDB).GetSQLDb()
	cryptoStore := crypto.NewSQLCryptoStore(sqlDB, dialect, "mastersword", []byte("masterswordpickle"), clients.CryptoMachineLogger{})
	if err := cryptoStore.AddOutboundGroupSession(ogsBot); err != nil {
		t.Errorf("Error storing bot OGS: %v", err)
	}

	if err := cryptoStore.PutGroupSession("!greatdekutree:hyrule", identityKeyMock, igsBot.ID(), igsBot); err != nil {
		t.Errorf("Error storing bot IGS: %v", err)
	}

	plaintext := `{"room_id":"!greatdekutree:hyrule","type":"m.room.message","content":{"body":"!echo save zelda","msgtype":"m.text"}}`
	ciphertext, err := ogsMock.Encrypt([]byte(plaintext))
	if err != nil {
		t.Errorf("Error encrypting bytes: %v", err)
	}

	// send neb an !echo message, encrypted with our mock OGS which it has an IGS for
	reqChan <- fmt.Sprintf(`{
		"next_batch":"11_22_33_44",
		"rooms": {
			"join": {"!greatdekutree:hyrule": {"timeline": {"events": [{
					"type": "m.room.encrypted",
					"sender": "@navi:hyrule",
					"content": {
						"algorithm":"m.megolm.v1.aes-sha2",
						"sender_key":"%s",
						"ciphertext":"%s",
						"session_id":"%s"
					},
					"origin_server_ts": 10000,
					"unsigned": {"age": 100},
					"event_id": "evt125"
				}]}}}
		}
	}`, identityKeyMock, string(ciphertext), ogsMock.ID())

	// wait for it to be processed
	reqChan <- `{"next_batch":"11_22_33_44", "rooms": {}}`

	expectedDecryptedMsg := `{"room_id":"!greatdekutree:hyrule","type":"m.room.message","content":{"msgtype":"m.notice","body":"save zelda"}}`
	if decryptedMsg != expectedDecryptedMsg {
		t.Errorf("Expected decrypted message to be `%v`, got `%v`", expectedDecryptedMsg, decryptedMsg)
	}
}
