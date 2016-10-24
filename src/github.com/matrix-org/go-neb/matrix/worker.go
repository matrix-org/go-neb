package matrix

import (
	log "github.com/Sirupsen/logrus"
	"runtime/debug"
)

// Worker processes incoming events and updates the Matrix client's data structures. It also informs
// any attached listeners of the new events.
type Worker struct {
	client    *Client
	listeners map[string][]OnEventListener // event type to listeners array
}

// OnEventListener can be used with Worker.OnEventType to be informed of incoming events.
type OnEventListener func(*Event)

func newWorker(client *Client) *Worker {
	return &Worker{
		client,
		make(map[string][]OnEventListener),
	}
}

// OnEventType allows callers to be notified when there are new events for the given event type.
// There are no duplicate checks.
func (worker *Worker) OnEventType(eventType string, callback OnEventListener) {
	_, exists := worker.listeners[eventType]
	if !exists {
		worker.listeners[eventType] = []OnEventListener{}
	}
	worker.listeners[eventType] = append(worker.listeners[eventType], callback)
}

func (worker *Worker) notifyListeners(event *Event) {
	listeners, exists := worker.listeners[event.Type]
	if !exists {
		return
	}
	for _, fn := range listeners {
		fn(event)
	}
}

func (worker *Worker) onSyncHTTPResponse(res syncHTTPResponse) {
	defer func() {
		if r := recover(); r != nil {
			userID := ""
			if worker.client != nil {
				userID = worker.client.UserID
			}
			log.WithFields(log.Fields{
				"panic":   r,
				"user_id": userID,
			}).Errorf(
				"onSyncHTTPResponse panicked!\n%s", debug.Stack(),
			)
		}
	}()

	for roomID, roomData := range res.Rooms.Join {
		room := worker.client.getOrCreateRoom(roomID)
		for _, event := range roomData.State.Events {
			event.RoomID = roomID
			room.UpdateState(&event)
			worker.notifyListeners(&event)
		}
		for _, event := range roomData.Timeline.Events {
			event.RoomID = roomID
			worker.notifyListeners(&event)
		}
	}
	for roomID, roomData := range res.Rooms.Invite {
		room := worker.client.getOrCreateRoom(roomID)
		for _, event := range roomData.State.Events {
			event.RoomID = roomID
			room.UpdateState(&event)
			worker.notifyListeners(&event)
		}
	}
}
