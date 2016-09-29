package services

import (
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"time"
)

type rssPoller struct{}

func (p *rssPoller) IntervalSecs() int64 { return 10 }
func (p *rssPoller) OnPoll(s types.Service) {
	rsss, ok := s.(*rssService)
	if !ok {
		log.WithField("service_id", s.ServiceID()).Error("RSS: OnPoll called without an RSS Service")
		return
	}
	now := time.Now().Unix() // Second resolution
	// URL => [ RoomID ]
	urlsToRooms := make(map[string][]string)

	for roomID, roomInfo := range rsss.Rooms {
		for u, feedInfo := range roomInfo.Feeds {
			if feedInfo.LastPollTimestampSecs == 0 || (feedInfo.LastPollTimestampSecs+(int64(feedInfo.PollIntervalMins)*60)) > now {
				// re-query this feed
				urlsToRooms[u] = append(urlsToRooms[u], roomID)
			}
		}
	}

	log.Print(rsss.ServiceID()+" Polly poll poll ", rsss.Rooms)
}

type rssService struct {
	types.DefaultService
	id            string
	serviceUserID string
	Rooms         map[string]struct { // room_id => {}
		Feeds map[string]struct { // URL => { }
			PollIntervalMins      int `json:"poll_interval_mins"`
			LastPollTimestampSecs int64
		}
	}
}

func (s *rssService) ServiceUserID() string { return s.serviceUserID }
func (s *rssService) ServiceID() string     { return s.id }
func (s *rssService) ServiceType() string   { return "rss" }
func (s *rssService) Poller() types.Poller  { return &rssPoller{} }

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *rssService) Register(oldService types.Service, client *matrix.Client) error {
	urlSet := make(map[string]bool)
	for _, roomInfo := range s.Rooms {
		for u, feedInfo := range roomInfo.Feeds {
			if feedInfo.PollIntervalMins == 0 {
				feedInfo.PollIntervalMins = 1
				log.Print("Set poll interval to 1 ", u)
			}
			urlSet[u] = true
		}
	}
	if len(urlSet) == 0 {
		log.Print(s.Rooms)
		return errors.New("An RSS feed must be specified.")
	}
	return nil
}

// PostRegister will start polling
func (s *rssService) PostRegister(oldService types.Service) {
	go polling.StartPolling(s, s.Poller())
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		r := &rssService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
		return r
	})
}
