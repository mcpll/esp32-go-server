package pool

import (
	"context"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

// StatsReporter resource pool stats reporter.
// Stop-gap: no transport until #19 writes to the PocketBase pool_stats record.
type StatsReporter struct {
	enabled bool
}

var (
	globalReporter *StatsReporter
	reporterOnce   sync.Once
)

func newStatsReporter() *StatsReporter {
	return &StatsReporter{enabled: viper.GetBool("pool_stats.report_enabled")}
}

// GetStatsReporter returns the global stats reporter (singleton)
func GetStatsReporter() *StatsReporter {
	reporterOnce.Do(func() {
		globalReporter = newStatsReporter()
		log.Infof("resource pool stats reporter initialized, enabled=%v", globalReporter.enabled)
	})
	return globalReporter
}

// StartReporting is a no-op until #19 adds the PocketBase transport.
func (r *StatsReporter) StartReporting(ctx context.Context) {
	if !r.enabled {
		log.Info("resource pool stats reporting disabled")
		return
	}
	log.Warn("resource pool stats reporting enabled but no transport is implemented yet (see #19)")
}

// StartStatsReporter starts the global stats reporter (convenience)
func StartStatsReporter(ctx context.Context) {
	GetStatsReporter().StartReporting(ctx)
}
