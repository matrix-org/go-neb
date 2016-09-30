package services

import (
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/polling"
	"github.com/matrix-org/go-neb/types"
	"github.com/mmcdole/gofeed"
	"time"
)

type feedPoller struct{}

func (p *feedPoller) IntervalSecs() int64 { return 10 }
func (p *feedPoller) OnPoll(s types.Service) {
	frService, ok := s.(*feedReaderService)
	if !ok {
		log.WithField("service_id", s.ServiceID()).Error("RSS: OnPoll called without an RSS Service")
		return
	}
	now := time.Now().Unix() // Second resolution
	// URL => [ RoomID ]
	urlsToRooms := make(map[string][]string)

	for roomID, roomInfo := range frService.Rooms {
		for u, feedInfo := range roomInfo.Feeds {
			if feedInfo.LastPollTimestampSecs == 0 || (feedInfo.LastPollTimestampSecs+(int64(feedInfo.PollIntervalMins)*60)) > now {
				// re-query this feed
				urlsToRooms[u] = append(urlsToRooms[u], roomID)
			}
		}
	}

	// TODO: Keep a "next poll ts" value (default 0)
	// If ts is 0 or now > ts, then poll and work out next poll ts.
	// Worked out by looking at the chosen interval period (prioritise the feed retry time where it exists)
	// Persist the next poll ts to the database.

	for u := range urlsToRooms {
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(u)
		if err != nil {
			log.WithFields(log.Fields{
				"service_id": s.ServiceID(),
				"url":        u,
				log.ErrorKey: err,
			}).Error("Failed to parse feed")
			continue
		}
		log.Print(feed)
	}

}

type feedReaderService struct {
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

func (s *feedReaderService) ServiceUserID() string { return s.serviceUserID }
func (s *feedReaderService) ServiceID() string     { return s.id }
func (s *feedReaderService) ServiceType() string   { return "feedreader" }
func (s *feedReaderService) Poller() types.Poller  { return &feedPoller{} }

// Register will check the liveness of each RSS feed given. If all feeds check out okay, no error is returned.
func (s *feedReaderService) Register(oldService types.Service, client *matrix.Client) error {
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

func (s *feedReaderService) PostRegister(oldService types.Service) {
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
	s, ok := srv.(*feedReaderService)
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
		r := &feedReaderService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
		return r
	})
}
