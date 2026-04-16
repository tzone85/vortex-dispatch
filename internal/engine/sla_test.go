package engine

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestMaxDurationFor_KnownComplexity(t *testing.T) {
	cfg := config.DefaultConfig().SLA

	tests := []struct {
		complexity int
		want       time.Duration
	}{
		{1, 60 * time.Minute},
		{3, 240 * time.Minute},
		{8, 960 * time.Minute},
	}
	for _, tt := range tests {
		got := MaxDurationFor(cfg, tt.complexity)
		if got != tt.want {
			t.Errorf("MaxDurationFor(%d) = %v, want %v", tt.complexity, got, tt.want)
		}
	}
}

func TestMaxDurationFor_UnknownComplexity_FallsBackToLower(t *testing.T) {
	cfg := config.DefaultConfig().SLA
	// 4 is not in default map; should use 3 (next lower) = 240min
	got := MaxDurationFor(cfg, 4)
	want := 240 * time.Minute
	if got != want {
		t.Errorf("MaxDurationFor(4) = %v, want %v (fallback to complexity 3)", got, want)
	}
}

func TestMaxDurationFor_EmptyConfig_ReturnsDefault(t *testing.T) {
	cfg := config.SLAConfig{}
	got := MaxDurationFor(cfg, 5)
	want := 60 * time.Minute
	if got != want {
		t.Errorf("MaxDurationFor with empty config = %v, want %v default", got, want)
	}
}

func TestCheckSLA_NotBreached(t *testing.T) {
	cfg := config.DefaultConfig().SLA
	startedAt := time.Now().Add(-30 * time.Minute) // 30 min ago
	if CheckSLA(cfg, 3, startedAt) {
		t.Error("expected not breached (30min < 240min)")
	}
}

func TestCheckSLA_Breached(t *testing.T) {
	cfg := config.DefaultConfig().SLA
	startedAt := time.Now().Add(-300 * time.Minute) // 5hr ago
	if !CheckSLA(cfg, 3, startedAt) {
		t.Error("expected breached (300min > 240min)")
	}
}

func TestCheckSLA_ZeroStartTime_NoBreach(t *testing.T) {
	cfg := config.DefaultConfig().SLA
	if CheckSLA(cfg, 3, time.Time{}) {
		t.Error("zero start time should not breach")
	}
}
