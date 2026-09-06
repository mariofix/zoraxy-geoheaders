package proxy

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mariofix/zoraxy-geoheaders/mod/config"
	"github.com/mariofix/zoraxy-geoheaders/mod/geoip"
	"github.com/mariofix/zoraxy-geoheaders/mod/stats"
	"github.com/mariofix/zoraxy-geoheaders/mod/zoraxy_plugin"
)

type SniffMetadata struct {
	UUID       string
	ClientIP   string
	Host       string
	URI        string
	Method     string
	RemoteAddr string
	Timestamp  time.Time
}

type ProxyEngine struct {
	cfgManager   *config.ConfigManager
	geoDB        *geoip.GeoDB
	tracker      *stats.Tracker
	transport    *http.Transport
	sniffMu      sync.RWMutex
	pendingSniff map[string]*SniffMetadata
	reqCounter   uint64
}

func NewProxyEngine(cfgManager *config.ConfigManager, geoDB *geoip.GeoDB, tracker *stats.Tracker) *ProxyEngine {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &ProxyEngine{
		cfgManager:   cfgManager,
		geoDB:        geoDB,
		tracker:      tracker,
		transport:    transport,
		pendingSniff: make(map[string]*SniffMetadata),
	}
}

// HandleSniff evaluates if a request should be captured and enriched with X-Country
func (p *ProxyEngine) HandleSniff(dsfr *zoraxy_plugin.DynamicSniffForwardRequest) zoraxy_plugin.SniffResult {
	cfg := p.cfgManager.Get()
	if !cfg.Enabled {
		return zoraxy_plugin.SniffResultSkip
	}

	clientIP := dsfr.RemoteAddr
	if len(dsfr.Header["Cf-Connecting-Ip"]) > 0 {
		clientIP = dsfr.Header["Cf-Connecting-Ip"][0]
	} else if len(dsfr.Header["X-Forwarded-For"]) > 0 {
		clientIP = strings.Split(dsfr.Header["X-Forwarded-For"][0], ",")[0]
	} else if len(dsfr.Header["X-Real-Ip"]) > 0 {
		clientIP = dsfr.Header["X-Real-Ip"][0]
	}

	meta := &SniffMetadata{
		UUID:       dsfr.GetRequestUUID(),
		ClientIP:   strings.TrimSpace(clientIP),
		Host:       dsfr.Host,
		URI:        dsfr.RequestURI,
		Method:     dsfr.Method,
		RemoteAddr: dsfr.RemoteAddr,
		Timestamp:  time.Now(),
	}

	if meta.UUID != "" {
		p.sniffMu.Lock()
		p.pendingSniff[meta.UUID] = meta
		p.sniffMu.Unlock()
	}

	return zoraxy_plugin.SniffResultAccept
}

// HandleCapture processes the captured request, injects X-Country header and proxies to upstream
func (p *ProxyEngine) HandleCapture(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	cfg := p.cfgManager.Get()

	// Extract request UUID if provided by Zoraxy
	reqUUID := r.Header.Get("X-Zoraxy-RequestID")
	var meta *SniffMetadata
	if reqUUID != "" {
		p.sniffMu.Lock()
		meta = p.pendingSniff[reqUUID]
		delete(p.pendingSniff, reqUUID)
		p.sniffMu.Unlock()
	}

	// Determine client IP
	clientIP := ""
	if meta != nil && meta.ClientIP != "" {
		clientIP = meta.ClientIP
	}
	if clientIP == "" {
		clientIP = geoip.ExtractClientIP(r)
	}

	// Resolve country
	countryCode := p.geoDB.ResolveIP(clientIP)
	if countryCode == "" {
		countryCode = cfg.FallbackCountry
	}

	// Header names
	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "X-Country"
	}

	// Inject X-Country header
	r.Header.Set(headerName, countryCode)

	if cfg.InjectContinent {
		continentName := cfg.ContinentHeaderName
		if continentName == "" {
			continentName = "X-Continent"
		}
		continentCode := resolveContinent(countryCode)
		r.Header.Set(continentName, continentCode)
	}

	// Determine upstream target
	upstreamTarget := p.getUpstreamTarget(r.Host, cfg)

	reqID := fmt.Sprintf("req-%d", atomic.AddUint64(&p.reqCounter, 1))

	if upstreamTarget == "" {
		// If no upstream target is configured (e.g. standalone test or header echo mode),
		// return response with confirmation and injected headers
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(headerName, countryCode)
		w.WriteHeader(http.StatusOK)
		respData := map[string]interface{}{
			"status":          "ok",
			"plugin":          "zoraxy-geoheaders",
			"client_ip":       clientIP,
			"injected_header": headerName,
			"country":         countryCode,
			"host":            r.Host,
			"uri":             r.RequestURI,
		}
		_ = json.NewEncoder(w).Encode(respData)

		duration := time.Since(start).Milliseconds()
		p.tracker.RecordRequest(stats.RequestLogEntry{
			ID:         reqID,
			Timestamp:  start,
			ClientIP:   clientIP,
			Host:       r.Host,
			Method:     r.Method,
			URI:        r.RequestURI,
			Country:    countryCode,
			StatusCode: http.StatusOK,
			DurationMs: duration,
		})
		return
	}

	// Proxy to upstream target
	targetURL, err := url.Parse(upstreamTarget)
	if err != nil {
		http.Error(w, "Bad Gateway: Invalid Upstream URL", http.StatusBadGateway)
		p.tracker.RecordRequest(stats.RequestLogEntry{
			ID:         reqID,
			Timestamp:  start,
			ClientIP:   clientIP,
			Host:       r.Host,
			Method:     r.Method,
			URI:        r.RequestURI,
			Country:    countryCode,
			StatusCode: http.StatusBadGateway,
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = p.transport

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.Header.Set(headerName, countryCode)
		if cfg.InjectContinent {
			req.Header.Set(cfg.ContinentHeaderName, resolveContinent(countryCode))
		}
		if clientIP != "" {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
			} else {
				req.Header.Set("X-Forwarded-For", clientIP)
			}
			if req.Header.Get("X-Real-IP") == "" {
				req.Header.Set("X-Real-IP", clientIP)
			}
		}
	}

	rw := &responseCaptureWriter{ResponseWriter: w, statusCode: http.StatusOK}
	proxy.ServeHTTP(rw, r)

	duration := time.Since(start).Milliseconds()
	p.tracker.RecordRequest(stats.RequestLogEntry{
		ID:         reqID,
		Timestamp:  start,
		ClientIP:   clientIP,
		Host:       r.Host,
		Method:     r.Method,
		URI:        r.RequestURI,
		Country:    countryCode,
		StatusCode: rw.statusCode,
		DurationMs: duration,
	})
}

func (p *ProxyEngine) getUpstreamTarget(host string, cfg config.Config) string {
	if host != "" {
		if u, ok := cfg.HostUpstreams[host]; ok && u != "" {
			return u
		}
		// Strip port if present
		if strings.Contains(host, ":") {
			hostOnly := strings.Split(host, ":")[0]
			if u, ok := cfg.HostUpstreams[hostOnly]; ok && u != "" {
				return u
			}
		}
	}
	return cfg.DefaultUpstream
}

type responseCaptureWriter struct {
	http.ResponseWriter
	statusCode int
	wroteHead  bool
}

func (w *responseCaptureWriter) WriteHeader(statusCode int) {
	if !w.wroteHead {
		w.statusCode = statusCode
		w.wroteHead = true
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

func (w *responseCaptureWriter) Write(b []byte) (int, error) {
	if !w.wroteHead {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func resolveContinent(countryCode string) string {
	// Simple mapping for prominent countries
	switch countryCode {
	case "US", "CA", "MX":
		return "NA"
	case "GB", "DE", "FR", "IT", "ES", "NL", "CH", "SE", "PL", "IE", "NO", "BE", "AT", "DK", "FI", "PT", "CZ", "GR", "RO", "UA":
		return "EU"
	case "JP", "CN", "KR", "TW", "HK", "SG", "IN", "ID", "TH", "MY", "VN", "PH", "AE", "SA", "IL":
		return "AS"
	case "AU", "NZ":
		return "OC"
	case "BR", "AR", "CL", "CO", "PE":
		return "SA"
	case "ZA", "EG", "NG", "KE", "MA":
		return "AF"
	default:
		return "OTHER"
	}
}
