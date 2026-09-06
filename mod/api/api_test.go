package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
)

func setupTestAPI(t *testing.T) (*APIHandler, *http.ServeMux, *config.ConfigManager, *geoip.GeoDB, *stats.Tracker) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgMgr, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	geoDB := geoip.NewGeoDB(&geoip.GeoDBOptions{})
	tracker := stats.NewTracker(50)
	handler := NewAPIHandler(cfgMgr, geoDB, tracker, "1.0.0")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "/geoheaders")

	return handler, mux, cfgMgr, geoDB, tracker
}

func TestAPIStatusAndStats(t *testing.T) {
	_, mux, _, _, tracker := setupTestAPI(t)

	tracker.RecordRequest(stats.RequestLogEntry{
		ID:         "1",
		ClientIP:   "8.8.8.8",
		Host:       "example.com",
		Method:     "GET",
		URI:        "/",
		Country:    "US",
		StatusCode: 200,
		DurationMs: 5,
	})

	// Test GET /geoheaders/api/status
	req := httptest.NewRequest("GET", "/geoheaders/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var statusResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}
	if statusResp["plugin"] != "zoraxy-geoheaders" {
		t.Errorf("expected plugin 'zoraxy-geoheaders', got %v", statusResp["plugin"])
	}

	// Test GET /geoheaders/api/stats
	req = httptest.NewRequest("GET", "/geoheaders/api/stats", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var statsResp stats.StatsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("failed to parse stats JSON: %v", err)
	}
	if statsResp.TotalRequests != 1 {
		t.Errorf("expected 1 total request, got %d", statsResp.TotalRequests)
	}
	if statsResp.CountryCounts["US"] != 1 {
		t.Errorf("expected 1 US request, got %d", statsResp.CountryCounts["US"])
	}

	// Test POST /geoheaders/api/stats/reset
	req = httptest.NewRequest("POST", "/geoheaders/api/stats/reset", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if tracker.GetSnapshot().TotalRequests != 0 {
		t.Errorf("expected 0 requests after reset, got %d", tracker.GetSnapshot().TotalRequests)
	}
}

func TestAPIConfigGetAndUpdate(t *testing.T) {
	_, mux, cfgMgr, _, _ := setupTestAPI(t)

	// GET config
	req := httptest.NewRequest("GET", "/geoheaders/api/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cfg config.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("failed to decode config: %v", err)
	}
	if cfg.HeaderName != "X-Country" {
		t.Errorf("expected HeaderName 'X-Country', got %s", cfg.HeaderName)
	}

	// POST config update
	cfg.HeaderName = "X-Geo-Location"
	cfg.DefaultUpstream = "http://127.0.0.1:3000"
	cfg.CustomOverrides = map[string]string{"10.200.0.0/16": "DC1"}

	bodyBytes, _ := json.Marshal(cfg)
	req = httptest.NewRequest("POST", "/geoheaders/api/config", bytes.NewReader(bodyBytes))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 on config update, got %d", rec.Code)
	}

	updated := cfgMgr.Get()
	if updated.HeaderName != "X-Geo-Location" {
		t.Errorf("expected updated HeaderName 'X-Geo-Location', got %s", updated.HeaderName)
	}
	if updated.DefaultUpstream != "http://127.0.0.1:3000" {
		t.Errorf("expected updated DefaultUpstream, got %s", updated.DefaultUpstream)
	}
	if updated.CustomOverrides["10.200.0.0/16"] != "DC1" {
		t.Errorf("expected custom override DC1, got %v", updated.CustomOverrides["10.200.0.0/16"])
	}
}

func TestAPILookupAndTest(t *testing.T) {
	_, mux, _, _, _ := setupTestAPI(t)

	// GET /geoheaders/api/lookup?ip=8.8.8.8
	req := httptest.NewRequest("GET", "/geoheaders/api/lookup?ip=8.8.8.8", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var lookupResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &lookupResp); err != nil {
		t.Fatalf("failed to parse lookup response: %v", err)
	}
	if lookupResp["country"] != "US" {
		t.Errorf("expected country US for 8.8.8.8, got %v", lookupResp["country"])
	}

	// POST /geoheaders/api/test
	testBody := `{"ip":"1.1.1.1","host":"test.com"}`
	req = httptest.NewRequest("POST", "/geoheaders/api/test", bytes.NewReader([]byte(testBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 on test, got %d", rec.Code)
	}

	var testResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("failed to parse test response: %v", err)
	}
	if testResp["country"] != "AU" {
		t.Errorf("expected country AU for 1.1.1.1, got %v", testResp["country"])
	}
}
