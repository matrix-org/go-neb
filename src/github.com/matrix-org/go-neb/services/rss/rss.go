package services

import (
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
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

	// TODO: Some polling
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
	feeds := feedUrls(s)
	if len(feeds) == 0 {
		// this is an error UNLESS the old service had some feeds in which case they are deleting us :(
		oldFeeds := feedUrls(oldService)
		if len(oldFeeds) == 0 {
			return errors.New("An RSS feed must be specified.")
		}
	}
	return nil
}

func (s *rssService) PostRegister(oldService types.Service) {
	if len(feedUrls(s)) == 0 { // bye-bye :(
		logger := log.WithFields(log.Fields{
			"service_id":   s.ServiceID(),
			"service_type": s.ServiceType(),
		})
		logger.Info("Deleting service (0 feeds)")
		polling.StopPolling(s)
		if err := database.GetServiceDB().DeleteService(s.ServiceID()); err != nil {
			logger.WithError(err).Error("Failed to delete service")
		}
	}
}

// feedUrls returns a list of feed urls for this service
func feedUrls(srv types.Service) []string {
	var feeds []string
	s, ok := srv.(*rssService)
	if !ok {
		return feeds
	}

	urlSet := make(map[string]bool)
	for _, roomInfo := range s.Rooms {
		for u := range roomInfo.Feeds {
			urlSet[u] = true
		}
	}

	for u := range urlSet {
		feeds = append(feeds, u)
	}
	return feeds
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
