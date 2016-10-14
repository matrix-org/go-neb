package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
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

// IncIncomingCommand increments the incoming command counter (TODO: cmd type)
func IncIncomingCommand() {
	numIncomingCmds.Inc()
}

// IncSuccessCommand increments the success command counter (TODO: cmd type)
func IncSuccessCommand() {
	numSuccessCmds.Inc()
}

// IncErrorCommand increments the error command counter (TODO: cmd type)
func IncErrorCommand() {
	numErrorCmds.Inc()
}

func init() {
	prometheus.MustRegister(numIncomingCmds)
	prometheus.MustRegister(numSuccessCmds)
	prometheus.MustRegister(numErrorCmds)
}
