package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
)

type APIHandler struct {
	cfgManager *config.ConfigManager
	geoDB      *geoip.GeoDB
	tracker    *stats.Tracker
	startTime  time.Time
	version    string
}

func NewAPIHandler(cfgManager *config.ConfigManager, geoDB *geoip.GeoDB, tracker *stats.Tracker, version string) *APIHandler {
	return &APIHandler{
		cfgManager: cfgManager,
		geoDB:      geoDB,
		tracker:    tracker,
		startTime:  time.Now(),
		version:    version,
	}
}

func (a *APIHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")

	mux.HandleFunc(prefix+"/api/status", a.handleStatus)
	mux.HandleFunc(prefix+"/api/config", a.handleConfig)
	mux.HandleFunc(prefix+"/api/stats", a.handleStats)
	mux.HandleFunc(prefix+"/api/stats/reset", a.handleStatsReset)
	mux.HandleFunc(prefix+"/api/lookup", a.handleLookup)
	mux.HandleFunc(prefix+"/api/test", a.handleTest)
}

func (a *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := a.cfgManager.Get()
	snap := a.tracker.GetSnapshot()

	status := map[string]interface{}{
		"plugin":         "zoraxy-geoheaders",
		"version":        a.version,
		"enabled":        cfg.Enabled,
		"uptime_seconds": int64(time.Since(a.startTime).Seconds()),
		"header_name":    cfg.HeaderName,
		"tag_filter":     cfg.TagFilter,
		"total_ranges":   a.geoDB.RangeCount(),
		"total_requests": snap.TotalRequests,
	}

	sendJSON(w, http.StatusOK, status)
}

func (a *APIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.cfgManager.Get()
		sendJSON(w, http.StatusOK, cfg)
	case http.MethodPost:
		var updateReq config.Config
		if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
			sendError(w, http.StatusBadRequest, "Invalid JSON body: "+err.Error())
			return
		}

		err := a.cfgManager.Update(func(c *config.Config) {
			c.Enabled = updateReq.Enabled
			if updateReq.HeaderName != "" {
				c.HeaderName = strings.TrimSpace(updateReq.HeaderName)
			}
			if updateReq.FallbackCountry != "" {
				c.FallbackCountry = strings.ToUpper(strings.TrimSpace(updateReq.FallbackCountry))
			}
			if updateReq.LocalCountry != "" {
				c.LocalCountry = strings.ToUpper(strings.TrimSpace(updateReq.LocalCountry))
			}
			c.InjectContinent = updateReq.InjectContinent
			if updateReq.ContinentHeaderName != "" {
				c.ContinentHeaderName = strings.TrimSpace(updateReq.ContinentHeaderName)
			}
			if updateReq.TagFilter != "" {
				c.TagFilter = strings.TrimSpace(updateReq.TagFilter)
			}
			c.DefaultUpstream = strings.TrimSpace(updateReq.DefaultUpstream)
			if updateReq.HostUpstreams != nil {
				c.HostUpstreams = updateReq.HostUpstreams
			}
			if updateReq.CustomOverrides != nil {
				c.CustomOverrides = updateReq.CustomOverrides
				// Reload custom overrides in GeoDB
				for cidr, country := range c.CustomOverrides {
					_ = a.geoDB.AddCustomOverride(cidr, country)
				}
			}
			if updateReq.LogLimit > 0 {
				c.LogLimit = updateReq.LogLimit
				a.tracker.SetMaxLogs(updateReq.LogLimit)
			}
			c.DebugMode = updateReq.DebugMode
		})

		if err != nil {
			sendError(w, http.StatusInternalServerError, "Failed to update config: "+err.Error())
			return
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"config":  a.cfgManager.Get(),
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *APIHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := a.tracker.GetSnapshot()
	sendJSON(w, http.StatusOK, snap)
}

func (a *APIHandler) handleStatsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.tracker.Reset()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Stats reset successfully",
	})
}

func (a *APIHandler) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ipStr := r.URL.Query().Get("ip")
	if ipStr == "" {
		ipStr = geoip.ExtractClientIP(r)
	}

	country := a.geoDB.ResolveIP(ipStr)
	if country == "" {
		cfg := a.cfgManager.Get()
		country = cfg.FallbackCountry
	}

	isPrivate := geoip.IsPrivateOrSpecialIP(ipStr)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ip":         ipStr,
		"country":    country,
		"is_private": isPrivate,
	})
}

func (a *APIHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var testReq struct {
		IP   string `json:"ip"`
		Host string `json:"host"`
	}

	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		sendError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if testReq.IP == "" {
		testReq.IP = geoip.ExtractClientIP(r)
	}

	cfg := a.cfgManager.Get()
	country := a.geoDB.ResolveIP(testReq.IP)
	if country == "" {
		country = cfg.FallbackCountry
	}

	headers := map[string]string{
		cfg.HeaderName: country,
	}
	if cfg.InjectContinent {
		headers[cfg.ContinentHeaderName] = country
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ip":               testReq.IP,
		"country":          country,
		"injected_headers": headers,
	})
}

func sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, statusCode int, message string) {
	sendJSON(w, statusCode, map[string]interface{}{
		"error":   true,
		"message": message,
	})
}
