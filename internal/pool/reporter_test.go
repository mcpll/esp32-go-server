package pool

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestStatsReporterDisabledByDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	r := newStatsReporter()
	if r.enabled {
		t.Fatal("reporter must be disabled when pool_stats.report_enabled is unset")
	}
	r.StartReporting(context.Background()) // must not panic or start anything

	viper.Set("pool_stats.report_enabled", true)
	if !newStatsReporter().enabled {
		t.Fatal("reporter must honour pool_stats.report_enabled=true")
	}
}
