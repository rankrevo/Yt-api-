package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"ytmp3api/internal/config"
	"ytmp3api/internal/handlers"
)

type Server struct {
	api  *handlers.API
	http *http.Server
}

func New() (*Server, error) {
	cfg := config.Load()
	api, err := handlers.NewAPI(cfg)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/", api.Router())

	h := &http.Server{Addr: ":8080", Handler: mux}
	return &Server{api: api, http: h}, nil
}

func (s *Server) Start() error {
	log.Printf("server starting on %s", s.http.Addr)
	return s.http.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fmt.Println("shutting down")
	return s.http.Shutdown(ctx)
}
