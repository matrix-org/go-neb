package matrix

import (
	"encoding/json"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// NEBStore implements the mautrix.Storer interface.
//
// It persists the next batch token in the database, and includes a ClientConfig for the client.
type NEBStore struct {
	mautrix.InMemoryStore
	Database     database.Storer
	ClientConfig api.ClientConfig
}

// SaveNextBatch saves to the database.
func (s *NEBStore) SaveNextBatch(userID id.UserID, nextBatch string) {
	if err := s.Database.UpdateNextBatch(userID, nextBatch); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"next_batch": nextBatch,
		}).Error("Failed to persist next_batch token")
	}
}

// LoadNextBatch loads from the database.
func (s *NEBStore) LoadNextBatch(userID id.UserID) string {
	token, err := s.Database.LoadNextBatch(userID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
		}).Error("Failed to load next_batch token")
		return ""
	}
	return token
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
