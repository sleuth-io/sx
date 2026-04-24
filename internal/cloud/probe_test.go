package cloud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestProbe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		// Peer should close immediately; we don't need to do anything.
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	cred := &Credential{
		RelayBaseURL: strings.TrimSuffix(srv.URL, "/") + "/relay/SRtest/",
		RelayGID:     "SRtest",
		MachineToken: "good-token",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Probe(ctx, cred, nil); err != nil {
		t.Fatalf("Probe should succeed: %v", err)
	}
}

func TestProbe_UnauthorizedMapsToSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Refuse the upgrade — handshake fails with HTTP 401.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cred := &Credential{
		RelayBaseURL: strings.TrimSuffix(srv.URL, "/") + "/relay/SRtest/",
		RelayGID:     "SRtest",
		MachineToken: "wrong-token",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Probe(ctx, cred, nil)
	if !errors.Is(err, ErrProbeUnauthorized) {
		t.Fatalf("expected ErrProbeUnauthorized, got %v", err)
	}
}

func TestProbe_InvalidCredential(t *testing.T) {
	if err := Probe(context.Background(), nil, nil); err == nil {
		t.Error("expected error for nil credential")
	}
	if err := Probe(context.Background(), &Credential{}, nil); err == nil {
		t.Error("expected error for empty credential")
	}
}
