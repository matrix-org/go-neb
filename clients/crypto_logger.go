package clients

import (
	log "github.com/sirupsen/logrus"
)

// CryptoMachineLogger wraps around the usual logger, implementing the Logger interface needed by OlmMachine.
type CryptoMachineLogger struct{}

// Error formats and logs an error message.
func (CryptoMachineLogger) Error(message string, args ...interface{}) {
	log.Errorf(message, args...)
}

// Warn formats and logs a warning message.
func (CryptoMachineLogger) Warn(message string, args ...interface{}) {
	log.Warnf(message, args...)
}

// Debug formats and logs a debug message.
func (CryptoMachineLogger) Debug(message string, args ...interface{}) {
	log.Debugf(message, args...)
}

// Trace formats and logs a trace message.
func (CryptoMachineLogger) Trace(message string, args ...interface{}) {
	log.Tracef(message, args...)
}
