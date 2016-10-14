package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/dugong"
	"github.com/matrix-org/go-neb/clients"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/polling"
	_ "github.com/matrix-org/go-neb/realms/github"
	_ "github.com/matrix-org/go-neb/realms/jira"
	"github.com/matrix-org/go-neb/server"
	_ "github.com/matrix-org/go-neb/services/echo"
	_ "github.com/matrix-org/go-neb/services/giphy"
	_ "github.com/matrix-org/go-neb/services/github"
	_ "github.com/matrix-org/go-neb/services/guggy"
	_ "github.com/matrix-org/go-neb/services/jira"
	_ "github.com/matrix-org/go-neb/services/rssbot"
	"github.com/matrix-org/go-neb/types"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
)

func main() {
	bindAddress := os.Getenv("BIND_ADDRESS")
	databaseType := os.Getenv("DATABASE_TYPE")
	databaseURL := os.Getenv("DATABASE_URL")
	baseURL := os.Getenv("BASE_URL")
	logDir := os.Getenv("LOG_DIR")

	if logDir != "" {
		log.AddHook(dugong.NewFSHook(
			filepath.Join(logDir, "info.log"),
			filepath.Join(logDir, "warn.log"),
			filepath.Join(logDir, "error.log"),
		))
	}

	log.Infof(
		"Go-NEB (BIND_ADDRESS=%s DATABASE_TYPE=%s DATABASE_URL=%s BASE_URL=%s LOG_DIR=%s)",
		bindAddress, databaseType, databaseURL, baseURL, logDir,
	)

	err := types.BaseURL(baseURL)
	if err != nil {
		log.WithError(err).Panic("Failed to get base url")
	}

	db, err := database.Open(databaseType, databaseURL)
	if err != nil {
		log.WithError(err).Panic("Failed to open database")
	}
	database.SetServiceDB(db)

	clients := clients.New(db)
	if err := clients.Start(); err != nil {
		log.WithError(err).Panic("Failed to start up clients")
	}

	http.Handle("/metrics", prometheus.Handler())
	http.Handle("/test", prometheus.InstrumentHandler("test", server.MakeJSONAPI(&heartbeatHandler{})))
	http.Handle("/admin/getService", prometheus.InstrumentHandler("getService", server.MakeJSONAPI(&getServiceHandler{db: db})))
	http.Handle("/admin/getSession", prometheus.InstrumentHandler("getSession", server.MakeJSONAPI(&getSessionHandler{db: db})))
	http.Handle("/admin/configureClient", prometheus.InstrumentHandler("configureClient", server.MakeJSONAPI(&configureClientHandler{db: db, clients: clients})))
	http.Handle("/admin/configureService", prometheus.InstrumentHandler("configureService", server.MakeJSONAPI(newConfigureServiceHandler(db, clients))))
	http.Handle("/admin/configureAuthRealm", prometheus.InstrumentHandler("configureAuthRealm", server.MakeJSONAPI(&configureAuthRealmHandler{db: db})))
	http.Handle("/admin/requestAuthSession", prometheus.InstrumentHandler("requestAuthSession", server.MakeJSONAPI(&requestAuthSessionHandler{db: db})))
	http.Handle("/admin/removeAuthSession", prometheus.InstrumentHandler("removeAuthSession", server.MakeJSONAPI(&removeAuthSessionHandler{db: db})))
	wh := &webhookHandler{db: db, clients: clients}
	http.HandleFunc("/services/hooks/", prometheus.InstrumentHandlerFunc("webhookHandler", wh.handle))
	rh := &realmRedirectHandler{db: db}
	http.HandleFunc("/realms/redirects/", prometheus.InstrumentHandlerFunc("realmRedirectHandler", rh.handle))

	polling.SetClients(clients)
	if err := polling.Start(); err != nil {
		log.WithError(err).Panic("Failed to start polling")
	}

	log.Fatal(http.ListenAndServe(bindAddress, nil))
}
