package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

type configFile struct {
	Clients []types.ClientConfig        `yaml:"clients"`
	Realms  []configureAuthRealmRequest `yaml:"realms"`
	// Sessions []sessionConfig             `yaml:"sessions"`
	// Services []serviceConfig             `yaml:"services"`
}

func loadFromConfig(db *database.ServiceDB, configFilePath string) (*configFile, error) {
	logger := log.WithField("config_file", configFilePath)
	logger.Info("Loading from config file")

	contents, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}
	var cfg configFile
	if err = yaml.Unmarshal(contents, &cfg); err != nil {
		return nil, err
	}

	// sanity check (at least 1 client and 1 service)
	if len(cfg.Clients) == 0 {
		return nil, fmt.Errorf("At least 1 client and 1 service must be specified")
	}

	return &cfg, nil
}
