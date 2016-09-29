package polling

import (
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"time"
)

var shouldPoll = make(map[string]bool) // Service ID => yay/nay

// Start polling already existing services
func Start() error {
	log.Print("Start polling")
	// Work out which service types require polling
	for serviceType, poller := range types.PollersByType() {
		if poller == nil {
			continue
		}
		// Query for all services with said service type
		srvs, err := database.GetServiceDB().LoadServicesByType(serviceType)
		if err != nil {
			return err
		}
		for _, s := range srvs {
			shouldPoll[s.ServiceID()] = true
			go StartPolling(s, poller)
		}
	}
	return nil
}

// StartPolling begins the polling loop for this service. Does not return, so call this
// as a goroutine!
func StartPolling(service types.Service, poller types.Poller) {
	for {
		if !shouldPoll[service.ServiceID()] {
			log.WithField("service_id", service.ServiceID()).Info("Terminating poll.")
			break
		}
		poller.OnPoll(service)
		time.Sleep(time.Duration(poller.IntervalSecs()) * time.Second)
	}
}
