package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Enabled             bool              `json:"enabled"`
	HeaderName          string            `json:"header_name"`
	FallbackCountry     string            `json:"fallback_country"`
	LocalCountry        string            `json:"local_country"`
	InjectContinent     bool              `json:"inject_continent"`
	ContinentHeaderName string            `json:"continent_header_name"`
	TagFilter           string            `json:"tag_filter"`
	DefaultUpstream     string            `json:"default_upstream"`
	HostUpstreams       map[string]string `json:"host_upstreams"`
	CustomOverrides     map[string]string `json:"custom_overrides"`
	LogLimit            int               `json:"log_limit"`
	DebugMode           bool              `json:"debug_mode"`
}

type ConfigManager struct {
	mu       sync.RWMutex
	filePath string
	cfg      Config
}

func DefaultConfig() Config {
	return Config{
		Enabled:             true,
		HeaderName:          "X-Country",
		FallbackCountry:     "XX",
		LocalCountry:        "LOCAL",
		InjectContinent:     false,
		ContinentHeaderName: "X-Continent",
		TagFilter:           "geo-match",
		DefaultUpstream:     "",
		HostUpstreams:       make(map[string]string),
		CustomOverrides:     make(map[string]string),
		LogLimit:            200,
		DebugMode:           false,
	}
}

func LoadConfig(filePath string) (*ConfigManager, error) {
	cm := &ConfigManager{
		filePath: filePath,
		cfg:      DefaultConfig(),
	}

	if filePath == "" {
		return cm, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = cm.Save()
			return cm, nil
		}
		return nil, err
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cm, nil
	}

	// Apply default values if fields are empty
	if loaded.HeaderName == "" {
		loaded.HeaderName = "X-Country"
	}
	if loaded.FallbackCountry == "" {
		loaded.FallbackCountry = "XX"
	}
	if loaded.LocalCountry == "" {
		loaded.LocalCountry = "LOCAL"
	}
	if loaded.ContinentHeaderName == "" {
		loaded.ContinentHeaderName = "X-Continent"
	}
	if loaded.TagFilter == "" {
		loaded.TagFilter = "geo-match"
	}
	if loaded.HostUpstreams == nil {
		loaded.HostUpstreams = make(map[string]string)
	}
	if loaded.CustomOverrides == nil {
		loaded.CustomOverrides = make(map[string]string)
	}
	if loaded.LogLimit <= 0 {
		loaded.LogLimit = 200
	}

	cm.cfg = loaded
	return cm, nil
}

func (cm *ConfigManager) Get() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	// Deep copy maps
	hostUpstreamsCopy := make(map[string]string, len(cm.cfg.HostUpstreams))
	for k, v := range cm.cfg.HostUpstreams {
		hostUpstreamsCopy[k] = v
	}
	overridesCopy := make(map[string]string, len(cm.cfg.CustomOverrides))
	for k, v := range cm.cfg.CustomOverrides {
		overridesCopy[k] = v
	}

	c := cm.cfg
	c.HostUpstreams = hostUpstreamsCopy
	c.CustomOverrides = overridesCopy
	return c
}

func (cm *ConfigManager) Update(fn func(c *Config)) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	fn(&cm.cfg)
	return cm.saveLocked()
}

func (cm *ConfigManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveLocked()
}

func (cm *ConfigManager) saveLocked() error {
	if cm.filePath == "" {
		return nil
	}
	dir := filepath.Dir(cm.filePath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	data, err := json.MarshalIndent(cm.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.filePath, data, 0644)
}
