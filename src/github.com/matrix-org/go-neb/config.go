package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
)

func loadFromConfig(db *database.ServiceDB, configFile string) error {
	logger := log.WithField("config_file", configFile)
	logger.Info("Loading from config file")
	return nil
}
