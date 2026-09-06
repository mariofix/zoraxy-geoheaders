package geoip

import (
	"net/http"
	"testing"
)

func TestGeoDBBasicLookups(t *testing.T) {
	db := NewGeoDB(&GeoDBOptions{
		FallbackCountryCode: "XX",
		LocalCountryCode:    "LOCAL",
	})

	testCases := []struct {
		ip       string
		expected string
	}{
		{"8.8.8.8", "US"},
		{"1.1.1.1", "AU"},
		{"127.0.0.1", "LOCAL"},
		{"192.168.1.50", "LOCAL"},
		{"10.200.0.1", "LOCAL"},
		{"::1", "LOCAL"},
		{"fe80::1", "LOCAL"},
		{"2001:4860:4860::8888", "US"},
		{"100.64.0.1", "LOCAL"},
	}

	for _, tc := range testCases {
		res := db.ResolveIP(tc.ip)
		if res != tc.expected {
			t.Errorf("ResolveIP(%s) = %s; want %s", tc.ip, res, tc.expected)
		}
	}
}

func TestGeoDBCustomOverrides(t *testing.T) {
	db := NewGeoDB(nil)

	err := db.SetCustomOverride("192.168.1.100", "MY")
	if err != nil {
		t.Fatalf("SetCustomOverride failed: %v", err)
	}

	err = db.SetCustomOverride("10.50.0.0/16", "SG")
	if err != nil {
		t.Fatalf("SetCustomOverride failed: %v", err)
	}

	if cc := db.ResolveIP("192.168.1.100"); cc != "MY" {
		t.Errorf("expected MY for 192.168.1.100, got %s", cc)
	}

	if cc := db.ResolveIP("10.50.1.2"); cc != "SG" {
		t.Errorf("expected SG for 10.50.1.2, got %s", cc)
	}

	if cc := db.ResolveIP("10.60.1.2"); cc != "LOCAL" {
		t.Errorf("expected LOCAL for 10.60.1.2, got %s", cc)
	}

	db.DeleteCustomOverride("192.168.1.100")
	if cc := db.ResolveIP("192.168.1.100"); cc != "LOCAL" {
		t.Errorf("expected LOCAL after delete, got %s", cc)
	}
}

func TestExtractClientIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	req.RemoteAddr = "203.0.113.195:54321"

	if ip := ExtractClientIP(req); ip != "203.0.113.195" {
		t.Errorf("expected 203.0.113.195, got %s", ip)
	}

	req.Header.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	if ip := ExtractClientIP(req); ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %s", ip)
	}

	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	if ip := ExtractClientIP(req); ip != "8.8.8.8" {
		t.Errorf("expected 8.8.8.8, got %s", ip)
	}
}
