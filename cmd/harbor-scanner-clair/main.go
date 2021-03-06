package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/goharbor/harbor-scanner-clair/pkg/clair"
	"github.com/goharbor/harbor-scanner-clair/pkg/etc"
	"github.com/goharbor/harbor-scanner-clair/pkg/http/api"
	v1 "github.com/goharbor/harbor-scanner-clair/pkg/http/api/v1"
	"github.com/goharbor/harbor-scanner-clair/pkg/persistence/redis"
	"github.com/goharbor/harbor-scanner-clair/pkg/registry"
	"github.com/goharbor/harbor-scanner-clair/pkg/scanner"
	"github.com/goharbor/harbor-scanner-clair/pkg/work"
	log "github.com/sirupsen/logrus"
)

var (
	// Default wise GoReleaser sets three ldflags:
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(etc.GetLogLevel())
	log.SetReportCaller(false)
	log.SetFormatter(&log.JSONFormatter{})

	config, err := etc.GetConfig()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	store := redis.NewStore(config.Store)

	workPool := work.New()

	log.WithFields(log.Fields{
		"version":  version,
		"commit":   commit,
		"built_at": date,
	}).Info("Starting harbor-scanner-clair")

	registryClientFactory := registry.NewClientFactory(config.TLS)

	clairClient, err := clair.NewClient(config.TLS, config.Clair)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	adapter := scanner.NewAdapter(registryClientFactory, clairClient, scanner.NewTransformer())

	enqueuer := scanner.NewEnqueuer(workPool, adapter, store)

	apiHandler := v1.NewAPIHandler(clairClient, enqueuer, store)

	apiServer := api.NewServer(config.API, apiHandler)

	shutdownComplete := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, syscall.SIGINT, syscall.SIGTERM)
		captured := <-sigint
		log.WithField("signal", captured.String()).Debug("Trapped os signal")

		apiServer.Shutdown(context.Background())
		workPool.Shutdown()

		close(shutdownComplete)
	}()

	workPool.Start()
	apiServer.ListenAndServe()

	<-shutdownComplete
}
