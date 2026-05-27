//go:build debug

package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/debughttp"
)

func startDebugServer(addr string) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           debughttp.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("debug listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("debug listen: %v", err)
		}
	}()
}
