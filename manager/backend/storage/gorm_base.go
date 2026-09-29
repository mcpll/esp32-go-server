package storage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// GormBaseStorage 通用GORM存储基类
// 包含所有基于GORM的存储操作的通用实现
type GormBaseStorage struct {
	DB *gorm.DB // 导出字段，允许子类访问
}

// NewGormBaseStorage 创建GORM基础存储实例
func NewGormBaseStorage(db *gorm.DB) *GormBaseStorage {
	return &GormBaseStorage{
		DB: db,
	}
}

// Ping 检查数据库连接
func (s *GormBaseStorage) Ping() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Ping()
}

// Close 关闭数据库连接
func (s *GormBaseStorage) Close() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}

// BeginTx 开始事务
func (s *GormBaseStorage) BeginTx(ctx context.Context) (Transaction, error) {
	tx := s.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	transaction := &GormTransaction{
		DB: tx,
	}
	transaction.init()
	return transaction, nil
}

// GormTransaction 通用GORM事务实现
type GormTransaction struct {
	DB *gorm.DB
	*GormUserStorage
	*GormDeviceStorage
	*GormAgentStorage
	*GormConfigStorage
}

// init 初始化事务中的存储组件
func (t *GormTransaction) init() {
	t.GormUserStorage = &GormUserStorage{db: t.DB}
	t.GormDeviceStorage = &GormDeviceStorage{db: t.DB}
	t.GormAgentStorage = &GormAgentStorage{db: t.DB}
	t.GormConfigStorage = &GormConfigStorage{db: t.DB}
}

// Commit 提交事务
func (t *GormTransaction) Commit() error {
	if err := t.DB.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Rollback 回滚事务
func (t *GormTransaction) Rollback() error {
	if err := t.DB.Rollback().Error; err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}
