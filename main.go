package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	apiToken = kingpin.Flag("buildkite-token", "Buildkite API token. Requires `read_builds` permissions.").Required().String()
	branch   = kingpin.Flag("branch", "GIT branches we are interested in. Can be defined multiple times.").Required().String()
	org      = kingpin.Flag("buildkite-org", "Buildkite organization which is to be scraped.").Required().String()
	port     = kingpin.Flag("port", "TCP port which the HTTP server should listen on.").Default("8080").Int()
)

func main() {
	kingpin.Parse()

	//buildkite.SetHttpDebug(true) // Useful when debugging.
	config, err := buildkite.NewTokenConfig(*apiToken, false)

	if err != nil {
		log.Fatal("Incorrect token:", err)
	}

	client := buildkite.NewClient(config.Client())
	client.UserAgent = "tink-buildkite-stats/v1.0.0"
	bk := NewInMemCachingBuildkite(&NetworkBuildkite{client, *org, *branch}, 5*time.Minute)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.DefaultLogger)
	r.Mount("/", (&Routes{bk}).Routes())

	log.Printf("Listening on port %d", *port)
	server := http.Server{Addr: fmt.Sprintf(":%d", *port), Handler: r}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}
