package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/dugong"
	"github.com/matrix-org/go-neb/api"
	"github.com/matrix-org/go-neb/api/handlers"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	_ "github.com/matrix-org/go-neb/metrics"
	"github.com/matrix-org/go-neb/polling"
	_ "github.com/matrix-org/go-neb/realms/github"
	_ "github.com/matrix-org/go-neb/realms/jira"
	_ "github.com/matrix-org/go-neb/services/alertmanager"
	_ "github.com/matrix-org/go-neb/services/echo"
	_ "github.com/matrix-org/go-neb/services/giphy"
	_ "github.com/matrix-org/go-neb/services/github"
	_ "github.com/matrix-org/go-neb/services/google"
	_ "github.com/matrix-org/go-neb/services/guggy"
	_ "github.com/matrix-org/go-neb/services/imgur"
	_ "github.com/matrix-org/go-neb/services/jira"
	_ "github.com/matrix-org/go-neb/services/rssbot"
	_ "github.com/matrix-org/go-neb/services/slackapi"
	_ "github.com/matrix-org/go-neb/services/travisci"
	_ "github.com/matrix-org/go-neb/services/wikipedia"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/util"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
	yaml "gopkg.in/yaml.v2"
)

// loadFromConfig loads a config file and returns a ConfigFile
func loadFromConfig(db *database.ServiceDB, configFilePath string) (*api.ConfigFile, error) {
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
	var c api.ConfigFile
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("Failed to convert to config file: %s", err)
	}

	// sanity check (at least 1 client and 1 service)
	if len(c.Clients) == 0 || len(c.Services) == 0 {
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

func insertServicesFromConfig(clis *clients.Clients, serviceReqs []api.ConfigureServiceRequest) error {
	for i, s := range serviceReqs {
		if err := s.Check(); err != nil {
			return fmt.Errorf("config: Service[%d] : %s", i, err)
		}
		service, err := types.CreateService(s.ID, s.Type, s.UserID, s.Config)
		if err != nil {
			return fmt.Errorf("config: Service[%d] : %s", i, err)
		}

		// Fetch the client for this service and register/poll
		c, err := clis.Client(s.UserID)
		if err != nil {
			return fmt.Errorf("config: Service[%d] : %s", i, err)
		}

		if err = service.Register(nil, c); err != nil {
			return fmt.Errorf("config: Service[%d] : %s", i, err)
		}
		if _, err := database.GetServiceDB().StoreService(service); err != nil {
			return fmt.Errorf("config: Service[%d] : %s", i, err)
		}
		service.PostRegister(nil)
	}
	return nil
}

func loadDatabase(databaseType, databaseURL, configYAML string) (*database.ServiceDB, error) {
	if configYAML != "" {
		databaseType = "sqlite3"
		databaseURL = ":memory:?_busy_timeout=5000"
	}

	db, err := database.Open(databaseType, databaseURL)
	if err == nil {
		database.SetServiceDB(db) // set singleton
	}
	return db, err
}

func setup(e envVars, mux *http.ServeMux, matrixClient *http.Client) {
	err := types.BaseURL(e.BaseURL)
	if err != nil {
		log.WithError(err).Panic("Failed to get base url")
	}

	db, err := loadDatabase(e.DatabaseType, e.DatabaseURL, e.ConfigFile)
	if err != nil {
		log.WithError(err).Panic("Failed to open database")
	}

	// Populate the database from the config file if one was supplied.
	var cfg *api.ConfigFile
	if e.ConfigFile != "" {
		if cfg, err = loadFromConfig(db, e.ConfigFile); err != nil {
			log.WithError(err).WithField("config_file", e.ConfigFile).Panic("Failed to load config file")
		}
		if err := db.InsertFromConfig(cfg); err != nil {
			log.WithError(err).Panic("Failed to persist config data into in-memory DB")
		}
		log.Info("Inserted ", len(cfg.Clients), " clients")
		log.Info("Inserted ", len(cfg.Realms), " realms")
		log.Info("Inserted ", len(cfg.Sessions), " sessions")
	}

	clients := clients.New(db, matrixClient)
	if err := clients.Start(); err != nil {
		log.WithError(err).Panic("Failed to start up clients")
	}

	// Handle non-admin paths for normal NEB functioning
	mux.Handle("/metrics", prometheus.Handler())
	mux.Handle("/test", prometheus.InstrumentHandler("test", util.MakeJSONAPI(&handlers.Heartbeat{})))
	wh := handlers.NewWebhook(db, clients)
	mux.HandleFunc("/services/hooks/", prometheus.InstrumentHandlerFunc("webhookHandler", util.Protect(wh.Handle)))
	rh := &handlers.RealmRedirect{db}
	mux.HandleFunc("/realms/redirects/", prometheus.InstrumentHandlerFunc("realmRedirectHandler", util.Protect(rh.Handle)))

	// Read exclusively from the config file if one was supplied.
	// Otherwise, add HTTP listeners for new Services/Sessions/Clients/etc.
	if e.ConfigFile != "" {
		if err := insertServicesFromConfig(clients, cfg.Services); err != nil {
			log.WithError(err).Panic("Failed to insert services")
		}

		log.Info("Inserted ", len(cfg.Services), " services")
	} else {
		mux.Handle("/admin/getService", prometheus.InstrumentHandler("getService", util.MakeJSONAPI(&handlers.GetService{db})))
		mux.Handle("/admin/getSession", prometheus.InstrumentHandler("getSession", util.MakeJSONAPI(&handlers.GetSession{db})))
		mux.Handle("/admin/configureClient", prometheus.InstrumentHandler("configureClient", util.MakeJSONAPI(&handlers.ConfigureClient{clients})))
		mux.Handle("/admin/configureService", prometheus.InstrumentHandler("configureService", util.MakeJSONAPI(handlers.NewConfigureService(db, clients))))
		mux.Handle("/admin/configureAuthRealm", prometheus.InstrumentHandler("configureAuthRealm", util.MakeJSONAPI(&handlers.ConfigureAuthRealm{db})))
		mux.Handle("/admin/requestAuthSession", prometheus.InstrumentHandler("requestAuthSession", util.MakeJSONAPI(&handlers.RequestAuthSession{db})))
		mux.Handle("/admin/removeAuthSession", prometheus.InstrumentHandler("removeAuthSession", util.MakeJSONAPI(&handlers.RemoveAuthSession{db})))
	}
	polling.SetClients(clients)
	if err := polling.Start(); err != nil {
		log.WithError(err).Panic("Failed to start polling")
	}
}

type envVars struct {
	BindAddress  string
	DatabaseType string
	DatabaseURL  string
	BaseURL      string
	LogDir       string
	ConfigFile   string
}

func main() {
	e := envVars{
		BindAddress:  os.Getenv("BIND_ADDRESS"),
		DatabaseType: os.Getenv("DATABASE_TYPE"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		BaseURL:      os.Getenv("BASE_URL"),
		LogDir:       os.Getenv("LOG_DIR"),
		ConfigFile:   os.Getenv("CONFIG_FILE"),
	}

	if e.LogDir != "" {
		log.AddHook(dugong.NewFSHook(
			filepath.Join(e.LogDir, "info.log"),
			filepath.Join(e.LogDir, "warn.log"),
			filepath.Join(e.LogDir, "error.log"),
			&log.TextFormatter{
				TimestampFormat:  "2006-01-02 15:04:05.000000",
				DisableColors:    true,
				DisableTimestamp: false,
				DisableSorting:   false,
			}, &dugong.DailyRotationSchedule{GZip: false},
		))
		log.SetOutput(ioutil.Discard)
	}

	log.Infof("Go-NEB (%+v)", e)

	setup(e, http.DefaultServeMux, http.DefaultClient)
	log.Fatal(http.ListenAndServe(e.BindAddress, nil))
}
