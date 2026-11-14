package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"notbot/internal/telemetry"
)

func TestKeepAlivePingsTheHealthPage(t *testing.T) {
	telemetry.Reset()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			hits.Add(1)
		}
	}))
	defer server.Close()

	startKeepAlive(server.URL+"/", 20*time.Millisecond, server.Client())
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() < 2 {
		t.Fatalf("expected repeated pings, got %d", hits.Load())
	}
	if telemetry.Snapshot()["counters"].(map[string]int64)["keepalive_ok"] < 2 {
		t.Fatal("successful pings were not counted")
	}
}

func TestKeepAliveIsOffWithoutRenderURL(t *testing.T) {
	startKeepAlive("", time.Millisecond, http.DefaultClient) // must return without starting a goroutine that panics
}
