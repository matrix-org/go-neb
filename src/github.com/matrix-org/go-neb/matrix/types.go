package matrix

import (
	"encoding/json"
	"html"
	"regexp"
)

// Room represents a single Matrix room.
type Room struct {
	ID       string
	State    map[string]map[string]*Event
	Timeline []Event
}

// UpdateState updates the room's current state with the given Event. This will clobber events based
// on the type/state_key combination.
func (room Room) UpdateState(event *Event) {
	_, exists := room.State[event.Type]
	if !exists {
		room.State[event.Type] = make(map[string]*Event)
	}
	room.State[event.Type][event.StateKey] = event
}

// GetStateEvent returns the state event for the given type/state_key combo, or nil.
func (room Room) GetStateEvent(eventType string, stateKey string) *Event {
	stateEventMap, _ := room.State[eventType]
	event, _ := stateEventMap[stateKey]
	return event
}

// GetMembershipState returns the membership state of the given user ID in this room. If there is
// no entry for this member, 'leave' is returned for consistency with left users.
func (room Room) GetMembershipState(userID string) string {
	state := "leave"
	event := room.GetStateEvent("m.room.member", userID)
	if event != nil {
		membershipState, found := event.Content["membership"]
		if found {
			mState, isString := membershipState.(string)
			if isString {
				state = mState
			}
		}
	}
	return state
}

// NewRoom creates a new Room with the given ID
func NewRoom(roomID string) *Room {
	// Init the State map and return a pointer to the Room
	return &Room{
		ID:    roomID,
		State: make(map[string]map[string]*Event),
	}
}

// Event represents a single Matrix event.
type Event struct {
	StateKey  string                 `json:"state_key"`        // The state key for the event. Only present on State Events.
	Sender    string                 `json:"sender"`           // The user ID of the sender of the event
	Type      string                 `json:"type"`             // The event type
	Timestamp int                    `json:"origin_server_ts"` // The unix timestamp when this message was sent by the origin server
	ID        string                 `json:"event_id"`         // The unique ID of this event
	RoomID    string                 `json:"room_id"`          // The room the event was sent to. May be nil (e.g. for presence)
	Content   map[string]interface{} `json:"content"`          // The JSON content of the event.
}

// Body returns the value of the "body" key in the event content if it is
// present and is a string.
func (event *Event) Body() (body string, ok bool) {
	value, exists := event.Content["body"]
	if !exists {
		return
	}
	body, ok = value.(string)
	return
}

// MessageType returns the value of the "msgtype" key in the event content if
// it is present and is a string.
func (event *Event) MessageType() (msgtype string, ok bool) {
	value, exists := event.Content["msgtype"]
	if !exists {
		return
	}
	msgtype, ok = value.(string)
	return
}

// TextMessage is the contents of a Matrix formated message event.
type TextMessage struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

type ImageInfo struct {
	Height   uint   `json:"h"`
	Width    uint   `json:"w"`
	Mimetype string `json:"mimetype"`
	Size     uint   `json:"size"`
}

// ImageMessage is an m.image event
type ImageMessage struct {
	MsgType string    `json:"msgtype"`
	Body    string    `json:"body"`
	URL     string    `json:"url"`
	Info    ImageInfo `json:"info"`
}

// An HTMLMessage is the contents of a Matrix HTML formated message event.
type HTMLMessage struct {
	Body          string `json:"body"`
	MsgType       string `json:"msgtype"`
	Format        string `json:"format"`
	FormattedBody string `json:"formatted_body"`
}

var htmlRegex = regexp.MustCompile("<[^<]+?>")

// GetHTMLMessage returns an HTMLMessage with the body set to a stripped version of the provided HTML, in addition
// to the provided HTML.
func GetHTMLMessage(msgtype, htmlText string) HTMLMessage {
	return HTMLMessage{
		Body:          html.UnescapeString(htmlRegex.ReplaceAllLiteralString(htmlText, "")),
		MsgType:       msgtype,
		Format:        "org.matrix.custom.html",
		FormattedBody: htmlText,
	}
}

// StarterLinkMessage represents a message with a starter_link custom data.
type StarterLinkMessage struct {
	Body string
	Link string
}

// MarshalJSON converts this message into actual event content JSON.
func (m StarterLinkMessage) MarshalJSON() ([]byte, error) {
	var data map[string]string

	if m.Link != "" {
		data = map[string]string{
			"org.matrix.neb.starter_link": m.Link,
		}
	}

	msg := struct {
		MsgType string            `json:"msgtype"`
		Body    string            `json:"body"`
		Data    map[string]string `json:"data,omitempty"`
	}{
		"m.notice", m.Body, data,
	}
	return json.Marshal(msg)
}
