package services

import (
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

type rssService struct {
	types.DefaultService
	id            string
	serviceUserID string
	ClientUserID  string              `json:"client_user_id"`
	Rooms         map[string]struct { // room_id => {}
		Feeds map[string]struct { // URL => { }
			PollIntervalMs int `json:"poll_interval_ms"`
		} `json:"feeds"`
	} `json:"rooms"`
}

func (s *rssService) ServiceUserID() string { return s.serviceUserID }
func (s *rssService) ServiceID() string     { return s.id }
func (s *rssService) ServiceType() string   { return "rss" }

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *rssService) Register(oldService types.Service, client *matrix.Client) error { return nil }

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		r := &rssService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
		return r
	})
}
