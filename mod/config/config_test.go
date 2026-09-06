package config

import (
	"path/filepath"
	"testing"
)

func TestConfigDefaultsAndSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cm, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	cfg := cm.Get()
	if !cfg.Enabled {
		t.Errorf("expected default Enabled=true, got %v", cfg.Enabled)
	}
	if cfg.HeaderName != "X-Country" {
		t.Errorf("expected default HeaderName='X-Country', got %v", cfg.HeaderName)
	}
	if cfg.FallbackCountry != "XX" {
		t.Errorf("expected default FallbackCountry='XX', got %v", cfg.FallbackCountry)
	}
	if cfg.TagFilter != "geo-match" {
		t.Errorf("expected default TagFilter='geo-match', got %v", cfg.TagFilter)
	}

	// Update config
	err = cm.Update(func(c *Config) {
		c.Enabled = false
		c.HeaderName = "X-Geo-Country"
		c.HostUpstreams["test.example.com"] = "http://127.0.0.1:8080"
		c.CustomOverrides["1.2.3.4/32"] = "ZZ"
	})
	if err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	// Reload from disk
	cm2, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	cfg2 := cm2.Get()
	if cfg2.Enabled {
		t.Errorf("expected Enabled=false after update, got %v", cfg2.Enabled)
	}
	if cfg2.HeaderName != "X-Geo-Country" {
		t.Errorf("expected HeaderName='X-Geo-Country', got %v", cfg2.HeaderName)
	}
	if cfg2.HostUpstreams["test.example.com"] != "http://127.0.0.1:8080" {
		t.Errorf("expected host upstream http://127.0.0.1:8080, got %v", cfg2.HostUpstreams["test.example.com"])
	}
	if cfg2.CustomOverrides["1.2.3.4/32"] != "ZZ" {
		t.Errorf("expected custom override ZZ, got %v", cfg2.CustomOverrides["1.2.3.4/32"])
	}
}
