package polling

import (
	"runtime/debug"
	"sync"
	"time"

	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
)

// Remember when we first started polling on this service ID. Polling routines will
// continually check this time. If the service gets updated, this will change, prompting
// older instances to die away. If this service gets removed, the time will be 0.
var (
	pollMutex     sync.Mutex
	startPollTime = make(map[string]int64) // ServiceID => unix timestamp
)
var clientPool *clients.Clients

// SetClients sets a pool of clients for passing into OnPoll
func SetClients(clis *clients.Clients) {
	clientPool = clis
}

// Start polling already existing services
func Start() error {
	// Work out which service types require polling
	for _, serviceType := range types.PollingServiceTypes() {
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
	// Set the poll time BEFORE spinning off the goroutine in case the caller immediately stops us. If we don't do this here,
	// we risk them setting the ts to 0 BEFORE we've set the start time, resulting in a poll when one was not intended.
	ts := time.Now().UnixNano()
	setPollStartTime(service, ts)
	go pollLoop(service, ts)
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
func pollLoop(service types.Service, ts int64) {
	logger := log.WithFields(log.Fields{
		"timestamp":    ts,
		"service_id":   service.ServiceID(),
		"service_type": service.ServiceType(),
	})

	defer func() {
		// Kill the poll loop entirely as it is likely that whatever made us panic will
		// make us panic again. We can whine bitterly about it though.
		if r := recover(); r != nil {
			logger.WithField("panic", r).Errorf(
				"pollLoop panicked!\n%s", debug.Stack(),
			)
		}
	}()

	poller, ok := service.(types.Poller)
	if !ok {
		logger.Error("Service is not a Poller.")
		return
	}
	logger.Info("Starting polling loop")
	cli, err := clientPool.Client(service.ServiceUserID())
	if err != nil {
		logger.WithError(err).WithField("user_id", service.ServiceUserID()).Error("Poll setup failed: failed to load client")
		return
	}
	for {
		logger.Info("OnPoll")
		nextTime := poller.OnPoll(cli)
		if pollTimeChanged(service, ts) {
			logger.Info("Terminating poll.")
			break
		}
		// work out how long to sleep
		if nextTime.Unix() == 0 {
			logger.Info("Terminating poll - OnPoll returned 0")
			break
		}
		now := time.Now()
		time.Sleep(nextTime.Sub(now))

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
