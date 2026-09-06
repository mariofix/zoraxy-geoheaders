package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mariofix/zoraxy-geoheaders/mod/api"
	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/proxy"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
	"github.com/mariofix/zoraxy-geoheaders/mod/zoraxy_plugin"
)

func setupTestServer(t *testing.T, backendURL string) (*httptest.Server, *config.ConfigManager, *stats.Tracker) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgMgr, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	_ = cfgMgr.Update(func(c *config.Config) {
		c.DefaultUpstream = backendURL
		c.InjectContinent = true
		c.CustomOverrides["203.0.113.0/24"] = "TEST_ZONE"
	})

	cfg := cfgMgr.Get()
	geoDB := geoip.NewGeoDB(&geoip.GeoDBOptions{
		FallbackCountryCode: cfg.FallbackCountry,
		LocalCountryCode:    cfg.LocalCountry,
	})

	for cidr, country := range cfg.CustomOverrides {
		_ = geoDB.AddCustomOverride(cidr, country)
	}

	tracker := stats.NewTracker(cfg.LogLimit)
	proxyEngine := proxy.NewProxyEngine(cfgMgr, geoDB, tracker)
	apiHandler := api.NewAPIHandler(cfgMgr, geoDB, tracker, PluginVersion)

	mux := http.NewServeMux()
	zoraxy_plugin.RegisterDynamicSniffHandler(mux, proxyEngine.HandleSniff)
	zoraxy_plugin.RegisterDynamicCaptureHandler(mux, proxyEngine.HandleCapture)
	apiHandler.RegisterRoutes(mux, "/")

	uiRouter := zoraxy_plugin.NewPluginEmbedUIRouter("geoheaders", &embeddedWeb, "www", "/")
	mux.Handle("/", uiRouter.Handler())

	server := httptest.NewServer(mux)
	return server, cfgMgr, tracker
}

func TestEndToEndZoraxyFlow(t *testing.T) {
	var receivedHeaders http.Header
	var receivedPath string

	// 1. Mock upstream backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"backend":"ok"}`)
	}))
	defer backend.Close()

	// 2. Start Plugin Server
	pluginServer, _, tracker := setupTestServer(t, backend.URL)
	defer pluginServer.Close()

	// 3. Test Dynamic Sniff Phase (/d_sniff/)
	sniffPayload := zoraxy_plugin.DynamicSniffForwardRequest{
		Method:     "GET",
		Hostname:   "example.com",
		URL:        "http://example.com/api/v1/user",
		RemoteAddr: "8.8.8.8:54321",
		Host:       "example.com",
		RequestURI: "/api/v1/user",
		Header: map[string][]string{
			"Cf-Connecting-Ip": {"8.8.8.8"},
		},
	}
	sniffBytes, _ := json.Marshal(sniffPayload)

	sniffReq, _ := http.NewRequest("POST", pluginServer.URL+"/d_sniff/", bytes.NewReader(sniffBytes))
	sniffReq.Header.Set("Content-Type", "application/json")
	sniffReq.Header.Set("X-Zoraxy-RequestID", "test-req-uuid-1")

	client := &http.Client{}
	sniffResp, err := client.Do(sniffReq)
	if err != nil {
		t.Fatalf("sniff request failed: %v", err)
	}
	defer sniffResp.Body.Close()

	if sniffResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for sniff, got %d", sniffResp.StatusCode)
	}
	body, _ := io.ReadAll(sniffResp.Body)
	if strings.TrimSpace(string(body)) != "OK" {
		t.Fatalf("expected 'OK' response, got '%s'", string(body))
	}

	// 4. Test Dynamic Capture Phase (/d_capture/)
	captureReq, _ := http.NewRequest("GET", pluginServer.URL+"/d_capture/api/v1/user", nil)
	captureReq.Header.Set("X-Zoraxy-RequestID", "test-req-uuid-1")
	captureReq.Header.Set("CF-Connecting-IP", "8.8.8.8")

	captureResp, err := client.Do(captureReq)
	if err != nil {
		t.Fatalf("capture request failed: %v", err)
	}
	defer captureResp.Body.Close()

	if captureResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from backend via proxy, got %d", captureResp.StatusCode)
	}

	// Verify backend received injected headers
	if receivedHeaders.Get("X-Country") != "US" {
		t.Errorf("expected X-Country: US, got '%s'", receivedHeaders.Get("X-Country"))
	}
	if receivedHeaders.Get("X-Continent") != "NA" {
		t.Errorf("expected X-Continent: NA, got '%s'", receivedHeaders.Get("X-Continent"))
	}
	if receivedPath != "/api/v1/user" {
		t.Errorf("expected backend path '/api/v1/user', got '%s'", receivedPath)
	}

	// Verify Stats
	snap := tracker.GetSnapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("expected 1 recorded request, got %d", snap.TotalRequests)
	}
	if snap.CountryCounts["US"] != 1 {
		t.Errorf("expected 1 US count, got %d", snap.CountryCounts["US"])
	}

	// 5. Test Web UI Serving
	uiReq, _ := http.NewRequest("GET", pluginServer.URL+"/", nil)
	uiResp, err := client.Do(uiReq)
	if err != nil {
		t.Fatalf("ui request failed: %v", err)
	}
	defer uiResp.Body.Close()

	if uiResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for UI, got %d", uiResp.StatusCode)
	}
	uiHTML, _ := io.ReadAll(uiResp.Body)
	if !strings.Contains(string(uiHTML), "Zoraxy GeoHeaders") {
		t.Errorf("UI does not contain 'Zoraxy GeoHeaders'")
	}

	// 6. Test Custom CIDR Override Flow
	captureReq2, _ := http.NewRequest("GET", pluginServer.URL+"/d_capture/custom", nil)
	captureReq2.Header.Set("X-Forwarded-For", "203.0.113.42")

	captureResp2, err := client.Do(captureReq2)
	if err != nil {
		t.Fatalf("capture request 2 failed: %v", err)
	}
	defer captureResp2.Body.Close()

	if receivedHeaders.Get("X-Country") != "TEST_ZONE" {
		t.Errorf("expected X-Country: TEST_ZONE for custom override, got '%s'", receivedHeaders.Get("X-Country"))
	}
}
