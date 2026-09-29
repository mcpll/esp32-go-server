package storage

import (
	"context"
	"gorm.io/gorm"
	"xiaozhi/manager/backend/models"
)

// GormAgentStorage 通用GORM智能体存储实现
type GormAgentStorage struct {
	db *gorm.DB
}

// NewGormAgentStorage 创建GORM智能体存储实例
func NewGormAgentStorage(db *gorm.DB) *GormAgentStorage {
	return &GormAgentStorage{
		db: db,
	}
}

// CreateAgent 创建智能体
func (s *GormAgentStorage) CreateAgent(ctx context.Context, agent *models.Agent) error {
	return s.db.WithContext(ctx).Create(agent).Error
}

// GetAgentByID 根据ID获取智能体
func (s *GormAgentStorage) GetAgentByID(ctx context.Context, id uint) (*models.Agent, error) {
	var agent models.Agent
	err := s.db.WithContext(ctx).First(&agent, id).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

// GetAgentsByUserID 根据用户ID获取智能体列表
func (s *GormAgentStorage) GetAgentsByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Agent, int64, error) {
	var agents []*models.Agent
	var total int64
	
	// 获取总数
	err := s.db.WithContext(ctx).Model(&models.Agent{}).Where("user_id = ?", userID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	
	// 获取分页数据
	err = s.db.WithContext(ctx).Where("user_id = ?", userID).Offset(offset).Limit(limit).Find(&agents).Error
	return agents, total, err
}

// UpdateAgent 更新智能体
func (s *GormAgentStorage) UpdateAgent(ctx context.Context, id uint, updates map[string]interface{}) error {
	return s.db.WithContext(ctx).Model(&models.Agent{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteAgent 删除智能体
func (s *GormAgentStorage) DeleteAgent(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&models.Agent{}, id).Error
}