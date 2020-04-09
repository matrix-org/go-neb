package matrix

import (
	"encoding/json"

	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/gomatrix"
	log "github.com/sirupsen/logrus"
)

// NEBStore implements the gomatrix.Storer interface.
//
// It persists the next batch token in the database, and includes a ClientConfig for the client.
type NEBStore struct {
	gomatrix.InMemoryStore
	Database     database.Storer
	ClientConfig api.ClientConfig
}

// SaveNextBatch saves to the database.
func (s *NEBStore) SaveNextBatch(userID, nextBatch string) {
	if err := s.Database.UpdateNextBatch(userID, nextBatch); err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"next_batch": nextBatch,
		}).Error("Failed to persist next_batch token")
	}
}

// LoadNextBatch loads from the database.
func (s *NEBStore) LoadNextBatch(userID string) string {
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
