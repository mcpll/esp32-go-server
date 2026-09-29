package storage

import (
	"context"
	"gorm.io/gorm"
	"xiaozhi/manager/backend/models"
)

// GormDeviceStorage 通用GORM设备存储实现
type GormDeviceStorage struct {
	db *gorm.DB
}

// NewGormDeviceStorage 创建GORM设备存储实例
func NewGormDeviceStorage(db *gorm.DB) *GormDeviceStorage {
	return &GormDeviceStorage{
		db: db,
	}
}

// CreateDevice 创建设备
func (s *GormDeviceStorage) CreateDevice(ctx context.Context, device *models.Device) error {
	return s.db.WithContext(ctx).Create(device).Error
}

// GetDeviceByID 根据ID获取设备
func (s *GormDeviceStorage) GetDeviceByID(ctx context.Context, id uint) (*models.Device, error) {
	var device models.Device
	err := s.db.WithContext(ctx).First(&device, id).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// GetDeviceByCode 根据设备码获取设备
func (s *GormDeviceStorage) GetDeviceByCode(ctx context.Context, deviceCode string) (*models.Device, error) {
	var device models.Device
	err := s.db.WithContext(ctx).Where("device_code = ?", deviceCode).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// GetDevicesByUserID 根据用户ID获取设备列表
func (s *GormDeviceStorage) GetDevicesByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Device, int64, error) {
	var devices []*models.Device
	var total int64
	
	// 获取总数
	err := s.db.WithContext(ctx).Model(&models.Device{}).Where("user_id = ?", userID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	
	// 获取分页数据
	err = s.db.WithContext(ctx).Where("user_id = ?", userID).Offset(offset).Limit(limit).Find(&devices).Error
	return devices, total, err
}

// UpdateDevice 更新设备
func (s *GormDeviceStorage) UpdateDevice(ctx context.Context, id uint, updates map[string]interface{}) error {
	return s.db.WithContext(ctx).Model(&models.Device{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteDevice 删除设备
func (s *GormDeviceStorage) DeleteDevice(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&models.Device{}, id).Error
}