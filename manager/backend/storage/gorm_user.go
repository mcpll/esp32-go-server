package storage

import (
	"context"
	"gorm.io/gorm"
	"xiaozhi/manager/backend/models"
)

// GormUserStorage 通用GORM用户存储实现
type GormUserStorage struct {
	db *gorm.DB
}

// NewGormUserStorage 创建GORM用户存储实例
func NewGormUserStorage(db *gorm.DB) *GormUserStorage {
	return &GormUserStorage{
		db: db,
	}
}

// CreateUser 创建用户
func (s *GormUserStorage) CreateUser(ctx context.Context, user *models.User) error {
	return s.db.WithContext(ctx).Create(user).Error
}

// GetUserByID 根据ID获取用户
func (s *GormUserStorage) GetUserByID(ctx context.Context, id uint) (*models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername 根据用户名获取用户
func (s *GormUserStorage) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail 根据邮箱获取用户
func (s *GormUserStorage) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUsers 获取用户列表
func (s *GormUserStorage) GetUsers(ctx context.Context, offset, limit int) ([]*models.User, int64, error) {
	var users []*models.User
	var total int64
	
	// 获取总数
	err := s.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	
	// 获取分页数据
	err = s.db.WithContext(ctx).Offset(offset).Limit(limit).Find(&users).Error
	return users, total, err
}

// UpdateUser 更新用户
func (s *GormUserStorage) UpdateUser(ctx context.Context, id uint, updates map[string]interface{}) error {
	return s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteUser 删除用户
func (s *GormUserStorage) DeleteUser(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&models.User{}, id).Error
}