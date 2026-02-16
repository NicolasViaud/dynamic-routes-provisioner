package certificate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	certacmehttp "github.com/nicol/dynamic-route-provisioner/cert-acme-http"
)

// Compile-time check.
var _ certacmehttp.ChallengeSolver = (*ChallengeServer)(nil)

// ChallengeServer serves ACME HTTP-01 challenge tokens via a lightweight
// HTTP server. It implements certacmehttp.ChallengeSolver.
type ChallengeServer struct {
	port   int
	mu     sync.Mutex
	tokens map[string]string // token → keyAuth
	server *http.Server
}

func NewChallengeServer(port int) *ChallengeServer {
	return &ChallengeServer{
		port:   port,
		tokens: make(map[string]string),
	}
}

// Present stores the token/keyAuth pair and ensures the HTTP server is running.
func (s *ChallengeServer) Present(_ context.Context, _, token, keyAuth string) error {
	s.mu.Lock()
	s.tokens[token] = keyAuth
	s.mu.Unlock()

	return s.ensureRunning()
}

// Cleanup removes the token entry.
func (s *ChallengeServer) Cleanup(_ context.Context, _, token string) error {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
	return nil
}

func (s *ChallengeServer) ensureRunning() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/acme-challenge/", s.handleChallenge)

	s.server = &http.Server{
		Handler: mux,
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", s.port, err)
	}

	go s.server.Serve(ln)
	return nil
}

func (s *ChallengeServer) handleChallenge(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Path[len("/.well-known/acme-challenge/"):]

	s.mu.Lock()
	keyAuth, ok := s.tokens[token]
	s.mu.Unlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(keyAuth))
}
