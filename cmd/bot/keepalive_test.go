package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

	stop := startKeepAlive(server.URL+"/", 20*time.Millisecond, server.Client(), func(time.Time) (bool, string) { return true, "test" }, nil)
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
	startKeepAlive("", time.Millisecond, http.DefaultClient, nil, nil)()
}

func TestKeepAliveStopsPingingOutsideWindowsAndAnnouncesSleepOnce(t *testing.T) {
	telemetry.Reset()
	var pings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { pings.Add(1) }))
	defer server.Close()
	var awakeNow atomic.Bool
	awakeNow.Store(true)
	sleeps := make(chan string, 10)
	stop := startKeepAlive(server.URL, 10*time.Millisecond, server.Client(),
		func(time.Time) (bool, string) {
			if awakeNow.Load() {
				return true, "dönem"
			}
			return false, "dönem dışı"
		},
		func(reason string) { sleeps <- reason })
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for keepAliveOK() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	awakeNow.Store(false)
	select {
	case reason := <-sleeps:
		if reason != "dönem dışı" {
			t.Fatalf("unexpected reason %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sleep was not announced")
	}
	before := pings.Load()
	time.Sleep(80 * time.Millisecond)
	if pings.Load() != before {
		t.Fatal("kept pinging outside the window")
	}
	if len(sleeps) != 0 {
		t.Fatal("sleep announced more than once")
	}
}
