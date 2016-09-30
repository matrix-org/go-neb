package polling

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"sync"
	"time"
)

// Remember when we first started polling on this service ID. Polling routines will
// continually check this time. If the service gets updated, this will change, prompting
// older instances to die away. If this service gets removed, the time will be 0.
var (
	pollMutex     sync.Mutex
	startPollTime = make(map[string]int64) // ServiceID => unix timestamp
)

// Start polling already existing services
func Start() error {
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
			if err := StartPolling(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// StartPolling begins a polling loop for this service.
// If one already exists for this service, it will be instructed to die. The new poll will not wait for this to happen,
// so there may be a brief period of overlap. It is safe to immediately call `StopPolling(service)` to immediately terminate
// this poll.
func StartPolling(service types.Service) error {
	p := types.PollersByType()[service.ServiceType()]
	if p == nil {
		return fmt.Errorf("Service %s (type=%s) doesn't have a Poller", service.ServiceID(), service.ServiceType())
	}
	// Set the poll time BEFORE spinning off the goroutine in case the caller immediately stops us. If we don't do this here,
	// we risk them setting the ts to 0 BEFORE we've set the start time, resulting in a poll when one was not intended.
	ts := time.Now().UnixNano()
	setPollStartTime(service, ts)
	go pollLoop(service, p, ts)
	return nil
}

// StopPolling stops all pollers for this service.
func StopPolling(service types.Service) {
	log.WithFields(log.Fields{
		"service_id":   service.ServiceID(),
		"service_type": service.ServiceType(),
	}).Info("StopPolling")
	setPollStartTime(service, 0)
}

// pollLoop begins the polling loop for this service. Does not return, so call this
// as a goroutine!
func pollLoop(service types.Service, poller types.Poller, ts int64) {
	logger := log.WithFields(log.Fields{
		"timestamp":     ts,
		"service_id":    service.ServiceID(),
		"service_type":  service.ServiceType(),
		"interval_secs": poller.IntervalSecs(),
	})
	logger.Info("Starting polling loop")
	for {
		poller.OnPoll(service)
		if pollTimeChanged(service, ts) {
			logger.Info("Terminating poll.")
			break
		}
		time.Sleep(time.Duration(poller.IntervalSecs()) * time.Second)
		if pollTimeChanged(service, ts) {
			logger.Info("Terminating poll.")
			break
		}
	}
}

// setPollStartTime clobbers the current poll time
func setPollStartTime(service types.Service, startTs int64) {
	pollMutex.Lock()
	defer pollMutex.Unlock()
	startPollTime[service.ServiceID()] = startTs
}

// pollTimeChanged returns true if the poll start time for this service ID is different to the one supplied.
func pollTimeChanged(service types.Service, ts int64) bool {
	pollMutex.Lock()
	defer pollMutex.Unlock()
	return startPollTime[service.ServiceID()] != ts
}
