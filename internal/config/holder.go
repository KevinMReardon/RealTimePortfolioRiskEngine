package config

import (
	"sync"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

// ConfigHolder holds the process-effective config (env + app_settings overlay).
type ConfigHolder struct {
	mu  sync.RWMutex
	cfg Config
}

func NewConfigHolder(cfg Config) *ConfigHolder {
	return &ConfigHolder{cfg: cfg}
}

func (h *ConfigHolder) Get() Config {
	if h == nil {
		return Config{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

func (h *ConfigHolder) Set(cfg Config) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
}

func (h *ConfigHolder) Policy() policy.Config {
	return h.Get().PolicyConfig()
}

func (h *ConfigHolder) TradingHalt() bool {
	return h.Get().TradingHalt
}
