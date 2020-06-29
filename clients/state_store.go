package clients

import (
	"errors"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// NebStateStore implements the StateStore interface for OlmMachine.
// It is used to determine which rooms are encrypted and which rooms are shared with a user.
// The state is updated by /sync responses.
type NebStateStore struct {
	Storer *mautrix.InMemoryStore
}

// IsEncrypted returns whether a room has been encrypted.
func (ss *NebStateStore) IsEncrypted(roomID id.RoomID) bool {
	room := ss.Storer.LoadRoom(roomID)
	if room == nil {
		return false
	}
	_, ok := room.State[event.StateEncryption]
	return ok
}

// FindSharedRooms returns a list of room IDs that the given user ID is also a member of.
func (ss *NebStateStore) FindSharedRooms(userID id.UserID) []id.RoomID {
	sharedRooms := make([]id.RoomID, 0)
	for roomID, room := range ss.Storer.Rooms {
		if room.GetMembershipState(userID) != event.MembershipLeave {
			sharedRooms = append(sharedRooms, roomID)
		}
	}
	return sharedRooms
}

// UpdateStateStore updates the internal state of NebStateStore from a /sync response.
func (ss *NebStateStore) UpdateStateStore(resp *mautrix.RespSync) {
	for roomID, evts := range resp.Rooms.Join {
		room := ss.Storer.LoadRoom(roomID)
		if room == nil {
			room = mautrix.NewRoom(roomID)
			ss.Storer.SaveRoom(room)
		}
		for _, i := range evts.State.Events {
			room.UpdateState(i)
		}
		for _, i := range evts.Timeline.Events {
			if i.Type.IsState() {
				room.UpdateState(i)
			}
		}
	}
}

// GetJoinedMembers returns a list of members that are currently in a room.
func (ss *NebStateStore) GetJoinedMembers(roomID id.RoomID) ([]id.UserID, error) {
	joinedMembers := make([]id.UserID, 0)
	room := ss.Storer.LoadRoom(roomID)
	if room == nil {
		return nil, errors.New("unknown roomID")
	}
	memberEvents := room.State[event.StateMember]
	if memberEvents == nil {
		return nil, errors.New("no state member events found")
	}
	for stateKey, stateEvent := range memberEvents {
		if stateEvent == nil {
			continue
		}
		stateEvent.Content.ParseRaw(event.StateMember)
		if stateEvent.Content.AsMember().Membership == event.MembershipJoin {
			joinedMembers = append(joinedMembers, id.UserID(stateKey))
		}
	}
	return joinedMembers, nil
}
