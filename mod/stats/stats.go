package stats

import (
	"sync"
	"time"
)

type RequestLogEntry struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	ClientIP   string    `json:"client_ip"`
	Host       string    `json:"host"`
	Method     string    `json:"method"`
	URI        string    `json:"uri"`
	Country    string    `json:"country"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
}

type StatsSnapshot struct {
	TotalRequests      uint64             `json:"total_requests"`
	CountryCounts      map[string]uint64  `json:"country_counts"`
	StatusCodeCounts   map[int]uint64     `json:"status_code_counts"`
	RecentRequests     []RequestLogEntry  `json:"recent_requests"`
	TopCountries       []CountryCountPair `json:"top_countries"`
	LastRequestTime    *time.Time         `json:"last_request_time,omitempty"`
}

type CountryCountPair struct {
	Country string `json:"country"`
	Count   uint64 `json:"count"`
}

type Tracker struct {
	mu            sync.RWMutex
	totalRequests uint64
	countryCounts map[string]uint64
	statusCounts  map[int]uint64
	recentLogs    []RequestLogEntry
	maxLogs       int
	lastRequest   *time.Time
}

func NewTracker(maxLogs int) *Tracker {
	if maxLogs <= 0 {
		maxLogs = 200
	}
	return &Tracker{
		countryCounts: make(map[string]uint64),
		statusCounts:  make(map[int]uint64),
		recentLogs:    make([]RequestLogEntry, 0, maxLogs),
		maxLogs:       maxLogs,
	}
}

func (t *Tracker) SetMaxLogs(maxLogs int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if maxLogs > 0 {
		t.maxLogs = maxLogs
		if len(t.recentLogs) > maxLogs {
			t.recentLogs = t.recentLogs[len(t.recentLogs)-maxLogs:]
		}
	}
}

func (t *Tracker) RecordRequest(entry RequestLogEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalRequests++
	now := entry.Timestamp
	if now.IsZero() {
		now = time.Now()
		entry.Timestamp = now
	}
	t.lastRequest = &now

	if entry.Country != "" {
		t.countryCounts[entry.Country]++
	} else {
		t.countryCounts["XX"]++
	}

	if entry.StatusCode > 0 {
		t.statusCounts[entry.StatusCode]++
	}

	// Add to ring buffer
	if len(t.recentLogs) >= t.maxLogs {
		// Shift left
		copy(t.recentLogs, t.recentLogs[1:])
		t.recentLogs[len(t.recentLogs)-1] = entry
	} else {
		t.recentLogs = append(t.recentLogs, entry)
	}
}

func (t *Tracker) GetSnapshot() StatsSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	countryCopy := make(map[string]uint64, len(t.countryCounts))
	for k, v := range t.countryCounts {
		countryCopy[k] = v
	}

	statusCopy := make(map[int]uint64, len(t.statusCounts))
	for k, v := range t.statusCounts {
		statusCopy[k] = v
	}

	logsCopy := make([]RequestLogEntry, len(t.recentLogs))
	// Return in reverse chronological order (newest first)
	for i, entry := range t.recentLogs {
		logsCopy[len(t.recentLogs)-1-i] = entry
	}

	// Calculate top countries
	pairs := make([]CountryCountPair, 0, len(t.countryCounts))
	for k, v := range t.countryCounts {
		pairs = append(pairs, CountryCountPair{Country: k, Count: v})
	}
	// Sort by count descending
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].Count > pairs[i].Count {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	var lastReq *time.Time
	if t.lastRequest != nil {
		tVal := *t.lastRequest
		lastReq = &tVal
	}

	return StatsSnapshot{
		TotalRequests:    t.totalRequests,
		CountryCounts:    countryCopy,
		StatusCodeCounts: statusCopy,
		RecentRequests:   logsCopy,
		TopCountries:     pairs,
		LastRequestTime:  lastReq,
	}
}

func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalRequests = 0
	t.countryCounts = make(map[string]uint64)
	t.statusCounts = make(map[int]uint64)
	t.recentLogs = make([]RequestLogEntry, 0, t.maxLogs)
	t.lastRequest = nil
}
