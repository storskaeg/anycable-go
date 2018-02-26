package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/anycable/anycable-go/node"
	"github.com/apex/log"

	"github.com/anycable/anycable-go/config"
)

// HTTPServer is wrapper over http.Server
type HTTPServer struct {
	node     *node.Node
	server   *http.Server
	addr     string
	secured  bool
	shutdown bool
	mu       sync.Mutex
	log      *log.Entry

	Mux *http.ServeMux
}

// NewServer builds HTTPServer from config params
func NewServer(node *node.Node, host string, port string, ssl *config.SSLOptions) (*HTTPServer, error) {
	mux := http.NewServeMux()
	addr := net.JoinHostPort(host, port)

	server := &http.Server{Addr: addr, Handler: mux}

	secured := ssl.Available()

	if secured {
		cer, err := tls.LoadX509KeyPair(ssl.CertPath, ssl.KeyPath)
		if err != nil {
			msg := fmt.Sprintf("Failed to load SSL certificate: %s.", err)
			return nil, errors.New(msg)
		}

		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}}
	}

	return &HTTPServer{
		node:     node,
		server:   server,
		addr:     addr,
		Mux:      mux,
		secured:  secured,
		shutdown: false,
		log:      log.WithField("context", "http"),
	}, nil
}

// Start server
func (s *HTTPServer) Start() error {
	if s.secured {
		s.log.Infof("Starting HTTPS server at %v", s.addr)
		return s.server.ListenAndServeTLS("", "")
	}

	s.log.Infof("Starting HTTP server at %v", s.addr)
	return s.server.ListenAndServe()
}

// Stop shuts down server gracefully.
// `wait`` specifies the amount of time to wait before
// closing active connections
func (s *HTTPServer) Stop(wait time.Duration) error {
	s.mu.Lock()
	if s.shutdown {
		return nil
	}
	s.shutdown = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	return s.server.Shutdown(ctx)
}
