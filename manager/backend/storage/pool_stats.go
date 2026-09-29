package storage

import (
	"sync"
	"time"
)

// PoolStatsData 资源池统计数据
type PoolStatsData struct {
	Timestamp time.Time              `json:"timestamp"`
	Stats     map[string]interface{} `json:"stats"`
}

// PoolStatsStorage 资源池统计存储（内存存储，只保存最新数据）
type PoolStatsStorage struct {
	mu       sync.RWMutex
	latest   *PoolStatsData // 只保存最新的统计数据
}

var (
	globalPoolStatsStorage *PoolStatsStorage
	once                   sync.Once
)

// GetPoolStatsStorage 获取全局资源池统计存储（单例）
func GetPoolStatsStorage() *PoolStatsStorage {
	once.Do(func() {
		globalPoolStatsStorage = &PoolStatsStorage{
			latest: nil,
		}
	})
	return globalPoolStatsStorage
}

// AddStats 添加统计数据（只保存最新的，覆盖旧数据）
func (s *PoolStatsStorage) AddStats(stats map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 直接覆盖最新数据
	s.latest = &PoolStatsData{
		Timestamp: time.Now(),
		Stats:     stats,
	}
}

// GetLatestStats 获取最新的统计数据
func (s *PoolStatsStorage) GetLatestStats() *PoolStatsData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latest == nil {
		return nil
	}

	// 返回最新数据的副本
	latest := *s.latest
	return &latest
}

// GetAllStats 获取所有统计数据（只返回最新的一条）
func (s *PoolStatsStorage) GetAllStats() []PoolStatsData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latest == nil {
		return []PoolStatsData{}
	}

	// 只返回最新的一条数据
	return []PoolStatsData{*s.latest}
}

// GetStatsByTimeRange 根据时间范围获取统计数据（只返回最新数据，如果时间范围内）
func (s *PoolStatsStorage) GetStatsByTimeRange(start, end time.Time) []PoolStatsData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latest == nil {
		return []PoolStatsData{}
	}

	// 检查最新数据是否在时间范围内
	if s.latest.Timestamp.After(start) && s.latest.Timestamp.Before(end) {
		return []PoolStatsData{*s.latest}
	}

	return []PoolStatsData{}
}

// GetStatsCount 获取当前存储的数据条数（只保存最新数据，所以返回0或1）
func (s *PoolStatsStorage) GetStatsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return 0
	}
	return 1
}
