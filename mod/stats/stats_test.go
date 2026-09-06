package stats

import (
	"testing"
	"time"
)

func TestTrackerRecordAndSnapshot(t *testing.T) {
	tracker := NewTracker(3)

	tracker.RecordRequest(RequestLogEntry{
		ID:         "1",
		Timestamp:  time.Now(),
		ClientIP:   "1.1.1.1",
		Host:       "a.com",
		Method:     "GET",
		URI:        "/",
		Country:    "AU",
		StatusCode: 200,
		DurationMs: 10,
	})

	tracker.RecordRequest(RequestLogEntry{
		ID:         "2",
		Timestamp:  time.Now(),
		ClientIP:   "8.8.8.8",
		Host:       "b.com",
		Method:     "POST",
		URI:        "/api",
		Country:    "US",
		StatusCode: 200,
		DurationMs: 15,
	})

	tracker.RecordRequest(RequestLogEntry{
		ID:         "3",
		Timestamp:  time.Now(),
		ClientIP:   "8.8.4.4",
		Host:       "b.com",
		Method:     "GET",
		URI:        "/api",
		Country:    "US",
		StatusCode: 404,
		DurationMs: 5,
	})

	snap := tracker.GetSnapshot()
	if snap.TotalRequests != 3 {
		t.Errorf("expected 3 total requests, got %d", snap.TotalRequests)
	}
	if snap.CountryCounts["US"] != 2 {
		t.Errorf("expected 2 US requests, got %d", snap.CountryCounts["US"])
	}
	if snap.CountryCounts["AU"] != 1 {
		t.Errorf("expected 1 AU request, got %d", snap.CountryCounts["AU"])
	}
	if snap.StatusCodeCounts[200] != 2 {
		t.Errorf("expected 2 200 responses, got %d", snap.StatusCodeCounts[200])
	}
	if snap.StatusCodeCounts[404] != 1 {
		t.Errorf("expected 1 404 response, got %d", snap.StatusCodeCounts[404])
	}
	if len(snap.RecentRequests) != 3 {
		t.Errorf("expected 3 recent requests, got %d", len(snap.RecentRequests))
	}
	// Newest should be first
	if snap.RecentRequests[0].ID != "3" {
		t.Errorf("expected newest entry to be ID 3, got %s", snap.RecentRequests[0].ID)
	}

	// Test ring buffer overflow
	tracker.RecordRequest(RequestLogEntry{
		ID:         "4",
		Timestamp:  time.Now(),
		ClientIP:   "192.168.1.1",
		Host:       "c.com",
		Method:     "GET",
		URI:        "/lan",
		Country:    "LOCAL",
		StatusCode: 200,
		DurationMs: 2,
	})

	snap2 := tracker.GetSnapshot()
	if snap2.TotalRequests != 4 {
		t.Errorf("expected 4 total requests, got %d", snap2.TotalRequests)
	}
	if len(snap2.RecentRequests) != 3 {
		t.Errorf("expected maxLogs 3 recent requests, got %d", len(snap2.RecentRequests))
	}
	if snap2.RecentRequests[0].ID != "4" {
		t.Errorf("expected newest entry to be ID 4, got %s", snap2.RecentRequests[0].ID)
	}

	// Test Reset
	tracker.Reset()
	snap3 := tracker.GetSnapshot()
	if snap3.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", snap3.TotalRequests)
	}
	if len(snap3.RecentRequests) != 0 {
		t.Errorf("expected 0 recent requests after reset, got %d", len(snap3.RecentRequests))
	}
}
