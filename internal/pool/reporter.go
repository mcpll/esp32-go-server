package pool

import (
	"context"
	"sync"
	"time"
	"xiaozhi-esp32-server-golang/internal/components/http"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

// StatsReporter resource pool stats reporter
type StatsReporter struct {
	client  *http.ManagerClient
	enabled bool
}

var (
	globalReporter *StatsReporter
	reporterOnce   sync.Once
)

// GetStatsReporter returns the global stats reporter (singleton)
func GetStatsReporter() *StatsReporter {
	reporterOnce.Do(func() {
		// Manager backend URL: prefer env, else config
		baseURL := util.GetBackendURL()
		if baseURL == "" {
			baseURL = "http://localhost:8080" // default
		}

		// Check whether reporting is enabled
		enabled := viper.GetBool("pool_stats.report_enabled")
		if !enabled {
			// Enabled by default
			enabled = true
		}

		// Create HTTP client
		managerClient := http.NewManagerClient(http.ManagerClientConfig{
			BaseURL:    baseURL,
			AuthToken:  util.GetManagerAuthToken(),
			Timeout:    5 * time.Second,
			MaxRetries: 2,
		})

		globalReporter = &StatsReporter{
			client:  managerClient,
			enabled: enabled,
		}

		log.Infof("resource pool stats reporter initialized, backend_url=%s, enabled=%v", baseURL, enabled)
	})
	return globalReporter
}

// StartReporting starts stats reporting (once every 5 seconds)
func (r *StatsReporter) StartReporting(ctx context.Context) {
	if !r.enabled {
		log.Info("resource pool stats reporting disabled")
		return
	}

	// Report interval (5 seconds)
	interval := viper.GetDuration("pool_stats.report_interval")
	if interval == 0 {
		interval = 5 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		//log.Infof("resource pool stats reporting started, every %v", interval)

		for {
			select {
			case <-ctx.Done():
				log.Debugf("resource pool stats reporting stopped")
				return
			case <-ticker.C:
				r.reportStats(ctx)
			}
		}
	}()
}

// reportStats reports stats data
func (r *StatsReporter) reportStats(ctx context.Context) {
	// Get stats
	stats := GetStats()

	// Skip if empty
	if len(stats) == 0 {
		//log.Debugf("no active resource pools, skip report")
		return
	}

	// Build request body
	requestBody := map[string]interface{}{
		"stats": stats,
	}

	// Send report request
	err := r.client.DoRequest(ctx, http.RequestOptions{
		Method: "POST",
		Path:   "/api/internal/pool/stats",
		Body:   requestBody,
	})

	if err != nil {
		log.Warnf("resource pool stats report failed: %v", err)
	} else {
		//log.Debugf("resource pool stats report ok, pool count: %d", len(stats))
	}
}

// StartStatsReporter starts the global stats reporter (convenience)
func StartStatsReporter(ctx context.Context) {
	reporter := GetStatsReporter()
	reporter.StartReporting(ctx)
}
