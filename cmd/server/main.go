package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
	githubapi "github.com/l-you/supasend-to-github-contents-proxy/internal/github"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	githubClient, err := githubapi.NewClient(cfg.GitHubAPIURL, cfg.GitHubToken, httpClient)
	if err != nil {
		log.Fatalf("create github client: %v", err)
	}

	if cfg.DebugListenAddr != "" {
		startDebugServer(cfg.DebugListenAddr)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(cfg, githubClient, httpClient),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}
