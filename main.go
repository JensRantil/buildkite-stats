package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	org := os.Getenv("BUILDKITE_ORGANIZATION")
	if org == "" {
		log.Fatalln("BUILDKITE_ORGANIZATIONenvironment variable must be set.")
	}

	branch := os.Getenv("BRANCH")

	apiToken := os.Getenv("BUILDKITE_API_TOKEN")
	if apiToken == "" {
		log.Fatalln("BUILDKITE_API_TOKEN environment variable must be set.")
	}
	//buildkite.SetHttpDebug(true) // Useful when debugging.
	config, err := buildkite.NewTokenConfig(apiToken, false)

	if err != nil {
		log.Fatal("Incorrect token:", err)
	}

	client := buildkite.NewClient(config.Client())
	client.UserAgent = "tink-buildkite-stats/v1.0.0"
	bk := NewCachingBuildkite(&NetworkBuildkite{client, org, branch}, 5*time.Minute)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.DefaultLogger)
	r.Mount("/", (&Routes{bk}).Routes())

	log.Printf("Listening on port %s", port)
	server := http.Server{Addr: fmt.Sprintf(":%s", port), Handler: r}
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}
