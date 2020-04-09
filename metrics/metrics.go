package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Status is the status of a measurable metric (incoming commands, outgoing polls, etc)
type Status string

// Common status values
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
)

var (
	cmdCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_pling_cmd_total",
		Help: "The number of incoming commands from matrix clients",
	}, []string{"cmd", "status"})
	configureServicesCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_configure_services_total",
		Help: "The total number of configured services requests",
	}, []string{"service_type"})
	webhookCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_webhook_total",
		Help: "The total number of recognised incoming webhook requests",
	}, []string{"service_type"})
	authSessionCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goneb_auth_session_total",
		Help: "The total number of successful /requestAuthSession requests",
	}, []string{"realm_type"})
)

// IncrementCommand increments the pling command counter
func IncrementCommand(cmdName string, st Status) {
	cmdCounter.With(prometheus.Labels{"cmd": cmdName, "status": string(st)}).Inc()
}

// IncrementConfigureService increments the /configureService counter
func IncrementConfigureService(serviceType string) {
	configureServicesCounter.With(prometheus.Labels{"service_type": serviceType}).Inc()
}

// IncrementWebhook increments the incoming webhook request counter
func IncrementWebhook(serviceType string) {
	webhookCounter.With(prometheus.Labels{"service_type": serviceType}).Inc()
}

// IncrementAuthSession increments the /requestAuthSession request counter
func IncrementAuthSession(realmType string) {
	authSessionCounter.With(prometheus.Labels{"realm_type": realmType}).Inc()
}

func init() {
	prometheus.MustRegister(cmdCounter)
	prometheus.MustRegister(configureServicesCounter)
	prometheus.MustRegister(webhookCounter)
	prometheus.MustRegister(authSessionCounter)
}
