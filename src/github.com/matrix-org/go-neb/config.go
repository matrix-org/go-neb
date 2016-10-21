package main

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

type configFile struct {
	Clients []types.ClientConfig
	Realms  []configureAuthRealmRequest
	// Sessions []sessionConfig             `yaml:"sessions"`
	// Services []serviceConfig
}

func loadFromConfig(db *database.ServiceDB, configFilePath string) (*configFile, error) {
	// ::Horrible hacks ahead::
	// The config is represented as YAML, and we want to convert that into NEB types.
	// However, NEB types make liberal use of json.RawMessage which the YAML parser
	// doesn't like. We can't implement MarshalYAML/UnmarshalYAML as a custom type easily
	// because YAML is insane and supports numbers as keys. The YAML parser therefore has the
	// generic form of map[interface{}]interface{} - but the JSON parser doesn't know
	// how to parse that.
	//
	// The hack that follows gets around this by type asserting all parsed YAML keys as
	// strings then re-encoding/decoding as JSON. That is:
	// YAML bytes -> map[interface]interface -> map[string]interface -> JSON bytes -> NEB types

	// Convert to YAML bytes
	contents, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}

	// Convert to map[interface]interface
	var cfg map[interface{}]interface{}
	if err = yaml.Unmarshal(contents, &cfg); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal YAML: %s", err)
	}

	// Convert to map[string]interface
	dict := convertKeysToStrings(cfg)

	// Convert to JSON bytes
	b, err := json.Marshal(dict)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal config as JSON: %s", err)
	}

	// Finally, Convert to NEB types
	var c configFile
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("Failed to convert to config file: %s", err)
	}
	log.Print(c.Realms)

	// sanity check (at least 1 client and 1 service)
	if len(c.Clients) == 0 {
		return nil, fmt.Errorf("At least 1 client and 1 service must be specified")
	}

	return &c, nil
}

func convertKeysToStrings(iface interface{}) interface{} {
	obj, isObj := iface.(map[interface{}]interface{})
	if isObj {
		strObj := make(map[string]interface{})
		for k, v := range obj {
			strObj[k.(string)] = convertKeysToStrings(v) // handle nested objects
		}
		return strObj
	}

	arr, isArr := iface.([]interface{})
	if isArr {
		for i := range arr {
			arr[i] = convertKeysToStrings(arr[i]) // handle nested objects
		}
		return arr
	}
	return iface // base type like string or number
}
