package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/pocketbase"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

// reportEvery is how often an enabled reporter overwrites the pool_stats record.
const reportEvery = 5 * time.Second

const (
	poolStatsCollection = "pool_stats"
	poolStatsKey        = "main"
	poolStatsFilter     = `key = "main"`
)

// StatsReporter writes resource pool stats to the single pool_stats record.
type StatsReporter struct {
	enabled bool
	client  *pocketbase.Client
	stats   func() map[string]any
	every   time.Duration
}

var (
	globalReporter *StatsReporter
	reporterOnce   sync.Once
)

func newStatsReporter() *StatsReporter {
	return &StatsReporter{enabled: viper.GetBool("pool_stats.report_enabled")}
}

func (r *StatsReporter) interval() time.Duration {
	if r.every > 0 {
		return r.every
	}
	return reportEvery
}

func (r *StatsReporter) snapshot() map[string]any {
	if r.stats != nil {
		if data := r.stats(); data != nil {
			return data
		}
	}
	stats := GetStats()
	if stats == nil {
		return map[string]any{}
	}
	return stats
}

// GetStatsReporter returns the global stats reporter (singleton)
func GetStatsReporter() *StatsReporter {
	reporterOnce.Do(func() {
		globalReporter = newStatsReporter()
		log.Infof("resource pool stats reporter initialized, enabled=%v", globalReporter.enabled)
	})
	return globalReporter
}

// StartReporting overwrites the pool_stats record every 5s when reporting is enabled.
// With pool_stats.report_enabled false it returns without sending anything.
func (r *StatsReporter) StartReporting(ctx context.Context) {
	if !r.enabled {
		log.Info("resource pool stats reporting disabled")
		return
	}
	log.Infof("resource pool stats reporter writing pool_stats every %s", r.interval())
	go r.loop(ctx)
}

func (r *StatsReporter) loop(ctx context.Context) {
	ticker := time.NewTicker(r.interval())
	defer ticker.Stop()
	r.publish(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.publish(ctx)
		}
	}
}

func (r *StatsReporter) publish(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	client, err := r.restClient()
	if err != nil {
		log.Warnf("pool stats: %v", err)
		return
	}
	if err := upsertPoolStats(ctx, client, r.snapshot()); err != nil {
		log.Warnf("pool stats: %v", err)
	}
}

func (r *StatsReporter) restClient() (*pocketbase.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	client, err := user_config.PocketBaseRESTClient()
	if err != nil {
		return nil, err
	}
	r.client = client
	return client, nil
}

func upsertPoolStats(ctx context.Context, client *pocketbase.Client, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	rec, err := client.First(ctx, poolStatsCollection, poolStatsFilter, "")
	if errors.Is(err, pocketbase.ErrNotFound) {
		_, err = client.Create(ctx, poolStatsCollection, map[string]any{
			"key":  poolStatsKey,
			"data": data,
		})
		return err
	}
	if err != nil {
		return err
	}
	id := rec.String("id")
	if id == "" {
		return fmt.Errorf("pool_stats record %q has no id", poolStatsKey)
	}
	return client.Update(ctx, poolStatsCollection, id, map[string]any{"data": data})
}

// StartStatsReporter starts the global stats reporter (convenience)
func StartStatsReporter(ctx context.Context) {
	GetStatsReporter().StartReporting(ctx)
}
