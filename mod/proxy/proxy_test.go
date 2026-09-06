package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
	"github.com/mariofix/zoraxy-geoheaders/mod/zoraxy_plugin"
)

func setupTestProxyEngine(t *testing.T) (*ProxyEngine, *config.ConfigManager, *geoip.GeoDB, *stats.Tracker) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgMgr, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	geoDB := geoip.NewGeoDB(&geoip.GeoDBOptions{})
	tracker := stats.NewTracker(100)
	engine := NewProxyEngine(cfgMgr, geoDB, tracker)

	return engine, cfgMgr, geoDB, tracker
}

func TestHandleSniff(t *testing.T) {
	engine, cfgMgr, _, _ := setupTestProxyEngine(t)

	req := &zoraxy_plugin.DynamicSniffForwardRequest{
		Host:       "example.com",
		RemoteAddr: "8.8.8.8:12345",
		Method:     "GET",
		RequestURI: "/api/test",
		Header: map[string][]string{
			"Cf-Connecting-Ip": {"8.8.8.8"},
		},
	}

	res := engine.HandleSniff(req)
	if res != zoraxy_plugin.SniffResultAccept {
		t.Errorf("expected SniffResultAccept, got %v", res)
	}

	// Disable plugin and verify skip
	_ = cfgMgr.Update(func(c *config.Config) {
		c.Enabled = false
	})

	resDisabled := engine.HandleSniff(req)
	if resDisabled != zoraxy_plugin.SniffResultSkip {
		t.Errorf("expected SniffResultSkip when disabled, got %v", resDisabled)
	}
}

func TestHandleCapture_NoUpstream(t *testing.T) {
	engine, _, _, tracker := setupTestProxyEngine(t)

	req := httptest.NewRequest("GET", "/test-path", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	rec := httptest.NewRecorder()

	engine.HandleCapture(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	countryHeader := rec.Header().Get("X-Country")
	if countryHeader != "US" {
		t.Errorf("expected X-Country US, got %s", countryHeader)
	}

	var respBody map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if respBody["country"] != "US" {
		t.Errorf("expected country US in body, got %v", respBody["country"])
	}

	summary := tracker.GetSnapshot()
	if summary.TotalRequests != 1 {
		t.Errorf("expected 1 total request in tracker, got %d", summary.TotalRequests)
	}
}

func TestHandleCapture_WithUpstream(t *testing.T) {
	engine, cfgMgr, _, tracker := setupTestProxyEngine(t)

	var receivedCountryHeader string
	var receivedContinentHeader string
	var receivedClientIP string

	// Setup mock backend
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCountryHeader = r.Header.Get("X-Country")
		receivedContinentHeader = r.Header.Get("X-Continent")
		receivedClientIP = r.Header.Get("X-Real-IP")

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "backend response")
	}))
	defer backendServer.Close()

	_ = cfgMgr.Update(func(c *config.Config) {
		c.DefaultUpstream = backendServer.URL
		c.InjectContinent = true
	})

	req := httptest.NewRequest("GET", "/proxied", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	rec := httptest.NewRecorder()

	engine.HandleCapture(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "backend response" {
		t.Errorf("expected backend response, got %s", rec.Body.String())
	}
	if receivedCountryHeader != "AU" {
		t.Errorf("expected backend to receive X-Country: AU, got %s", receivedCountryHeader)
	}
	if receivedContinentHeader != "OC" {
		t.Errorf("expected backend to receive X-Continent: OC, got %s", receivedContinentHeader)
	}
	if receivedClientIP != "1.1.1.1" {
		t.Errorf("expected X-Real-IP: 1.1.1.1, got %s", receivedClientIP)
	}

	summary := tracker.GetSnapshot()
	if summary.TotalRequests != 1 {
		t.Errorf("expected 1 request in tracker, got %d", summary.TotalRequests)
	}
	if summary.CountryCounts["AU"] != 1 {
		t.Errorf("expected 1 AU request in country counts, got %d", summary.CountryCounts["AU"])
	}
}
