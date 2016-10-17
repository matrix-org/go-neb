package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type CommandStatus int

const (
	Pending CommandStatus = iota
	Success
	Failure
)

var (
	numIncomingCmds = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "num_incoming_commands_total",
		Help: "The number of incoming commands from matrix clients",
	})
	numSuccessCmds = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "num_success_commands_total",
		Help: "The number of incoming commands from matrix clients which were successful",
	})
	numErrorCmds = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "num_error_commands_total",
		Help: "The number of incoming commands from matrix clients which failed",
	})
)

// IncrementCommand increments the incoming command counter (TODO: cmd type)
func IncrementCommand(st CommandStatus) {
	switch st {
	case Pending:
		numIncomingCmds.Inc()
	case Success:
		numSuccessCmds.Inc()
	case Failure:
		numErrorCmds.Inc()
	}
}

func init() {
	prometheus.MustRegister(numIncomingCmds)
	prometheus.MustRegister(numSuccessCmds)
	prometheus.MustRegister(numErrorCmds)
}
