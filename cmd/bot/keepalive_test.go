package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"notbot/internal/telemetry"
)

func keepAliveOK() int64 { return telemetry.Snapshot()["counters"].(map[string]int64)["keepalive_ok"] }

func TestKeepAlivePingsTheHealthPage(t *testing.T) {
	telemetry.Reset()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	stop := startKeepAlive(server.URL+"/", 20*time.Millisecond, server.Client())
	defer stop()
	// Sayaç istemci cevabı aldıktan sonra artar; sunucu tarafını değil sayacı bekle.
	deadline := time.Now().Add(2 * time.Second)
	for keepAliveOK() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if keepAliveOK() < 2 {
		t.Fatalf("expected at least two counted pings, got %d", keepAliveOK())
	}
}

func TestKeepAliveIsOffWithoutRenderURL(t *testing.T) {
	startKeepAlive("", time.Millisecond, http.DefaultClient)()
}
