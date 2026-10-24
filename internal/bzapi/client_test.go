package bzapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFetchFixture(t *testing.T) {
	body, err := os.ReadFile("../../tests/fixtures/bzminer-snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/snapshot" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	snap, err := New(srv.URL, time.Second).Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snap.Devices) != 2 {
		t.Fatalf("devices = %d", len(snap.Devices))
	}
	if snap.Devices[0].Name != "GRID P4-8Q" {
		t.Errorf("name = %q", snap.Devices[0].Name)
	}
	if m := snap.Devices[0].Extra["memory.dedicated.total"]; m.Value != 8192 {
		t.Errorf("memory = %+v", m)
	}
	if !snap.ConnectedPool() {
		t.Error("ConnectedPool = false")
	}
	if snap.Pools[0].URL != "stratum+tcp://pearl-au.luckypool.io:3360" {
		t.Errorf("pool url = %q", snap.Pools[0].URL)
	}
}

func TestFetchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad-status":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"type":"not-snapshot"}`))
		}
	}))
	defer srv.Close()

	if _, err := New(srv.URL, time.Second).Fetch(t.Context()); err == nil {
		t.Fatal("expected unexpected-type error")
	}
	if _, err := New(srv.URL+"/bad-status", time.Second).Fetch(t.Context()); err == nil {
		t.Fatal("expected HTTP error")
	}
	if _, err := New("http://127.0.0.1:1", time.Second).Fetch(t.Context()); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestFetchRejectsHugeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"snapshot","pad":"` + strings.Repeat("x", maxBody+1) + `"}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, time.Second).Fetch(t.Context()); err == nil {
		t.Fatal("expected decode error for oversized body")
	}
}

var _ = context.Background
