package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// Server wraps http.Server with graceful shutdown.
type Server struct {
	srv *http.Server
	log logger.Logger
}

// NewServer creates a new Server bound to host:port using the given handler.
func NewServer(host string, port int, handler http.Handler, log logger.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", host, port),
			Handler:      handler,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		log: log,
	}
}

// ListenAndServe starts the server and blocks until a shutdown signal is received.
// It handles SIGINT and SIGTERM for graceful shutdown.
func (s *Server) ListenAndServe() error {
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		s.log.Info("server.start", map[string]interface{}{
			"addr": s.srv.Addr,
		})
		serverErr <- s.srv.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case sig := <-shutdown:
		s.log.Info("server.shutdown", map[string]interface{}{
			"signal": sig.String(),
		})

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := s.srv.Shutdown(ctx); err != nil {
			s.srv.Close()
			return fmt.Errorf("server: graceful shutdown failed: %w", err)
		}
	}
	return nil
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.srv.Addr
}

// ListenAndServeWithListener starts the server on the provided listener.
// Used for testing. Does not install signal handlers.
func (s *Server) ListenAndServeWithListener(ln net.Listener) error {
	return s.srv.Serve(ln)
}
