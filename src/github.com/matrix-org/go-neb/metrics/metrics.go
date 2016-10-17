package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// CommandStatus is the status of a incoming command
type CommandStatus int

// The command status values
const (
	StatusPending CommandStatus = iota
	StatusSuccess
	StatusFailure
)

var (
	numIncomingCmds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "num_incoming_commands_total",
		Help: "The number of incoming commands from matrix clients",
	}, []string{"cmd"})
	numSuccessCmds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "num_success_commands_total",
		Help: "The number of incoming commands from matrix clients which were successful",
	}, []string{"cmd"})
	numErrorCmds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "num_error_commands_total",
		Help: "The number of incoming commands from matrix clients which failed",
	}, []string{"cmd"})
)

// IncrementCommand increments the incoming command counter
func IncrementCommand(cmdName string, st CommandStatus) {
	switch st {
	case StatusPending:
		numIncomingCmds.With(prometheus.Labels{"cmd": cmdName}).Inc()
	case StatusSuccess:
		numSuccessCmds.With(prometheus.Labels{"cmd": cmdName}).Inc()
	case StatusFailure:
		numErrorCmds.With(prometheus.Labels{"cmd": cmdName}).Inc()
	}
}

func init() {
	prometheus.MustRegister(numIncomingCmds)
	prometheus.MustRegister(numSuccessCmds)
	prometheus.MustRegister(numErrorCmds)
}
