package engine

import (
	"sort"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// defaultSLAFallback is used when no SLA config is provided.
const defaultSLAFallback = 60 * time.Minute

// MaxDurationFor returns the maximum allowed story duration for the given
// complexity. If the exact complexity is not configured, falls back to the
// nearest lower configured complexity. If no config exists, returns 60min.
func MaxDurationFor(cfg config.SLAConfig, complexity int) time.Duration {
	if len(cfg.MaxMinutesPerComplexity) == 0 {
		return defaultSLAFallback
	}
	if minutes, ok := cfg.MaxMinutesPerComplexity[complexity]; ok {
		return time.Duration(minutes) * time.Minute
	}
	// Find nearest lower complexity
	keys := make([]int, 0, len(cfg.MaxMinutesPerComplexity))
	for k := range cfg.MaxMinutesPerComplexity {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	best := -1
	for _, k := range keys {
		if k <= complexity {
			best = k
		}
	}
	if best < 0 {
		return defaultSLAFallback
	}
	return time.Duration(cfg.MaxMinutesPerComplexity[best]) * time.Minute
}

// CheckSLA returns true if the elapsed time since startedAt exceeds the
// max duration for the given complexity. Returns false if startedAt is zero.
func CheckSLA(cfg config.SLAConfig, complexity int, startedAt time.Time) bool {
	if startedAt.IsZero() {
		return false
	}
	maxDuration := MaxDurationFor(cfg, complexity)
	return time.Since(startedAt) > maxDuration
}
