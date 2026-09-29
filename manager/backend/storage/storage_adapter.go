package storage

import (
	"context"
	"xiaozhi/manager/backend/models"
)

// StorageAdapter 存储适配器，用于桥接接口差异
type StorageAdapter struct {
	*GormBaseStorage
	userStorage   *GormUserStorage
	deviceStorage *GormDeviceStorage
	agentStorage  *GormAgentStorage
	configAdapter *ConfigAdapter
}

// NewStorageAdapter 创建存储适配器
func NewStorageAdapter(base *GormBaseStorage) *StorageAdapter {
	configStorage := NewGormConfigStorage(base.DB)
	return &StorageAdapter{
		GormBaseStorage: base,
		userStorage:     NewGormUserStorage(base.DB),
		deviceStorage:   NewGormDeviceStorage(base.DB),
		agentStorage:    NewGormAgentStorage(base.DB),
		configAdapter:   NewConfigAdapter(configStorage),
	}
}

// Connect 连接数据库（适配器方法）
func (a *StorageAdapter) Connect(ctx context.Context) error {
	// 基类已经连接，这里只是接口适配
	return nil
}

// Ping 检查数据库连接（适配器方法）
func (a *StorageAdapter) Ping(ctx context.Context) error {
	return a.GormBaseStorage.Ping()
}

// UserStorage 返回用户存储接口
func (a *StorageAdapter) UserStorage() UserStorage {
	return a.userStorage
}

// CreateUser 创建用户
func (a *StorageAdapter) CreateUser(ctx context.Context, user *models.User) error {
	return a.userStorage.CreateUser(ctx, user)
}

// GetUsers 获取所有用户
func (a *StorageAdapter) GetUsers(ctx context.Context, offset, limit int) ([]*models.User, int64, error) {
	return a.userStorage.GetUsers(ctx, offset, limit)
}

// GetUserByID 根据ID获取用户
func (a *StorageAdapter) GetUserByID(ctx context.Context, id uint) (*models.User, error) {
	return a.userStorage.GetUserByID(ctx, id)
}

// GetUserByUsername 根据用户名获取用户
func (a *StorageAdapter) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	return a.userStorage.GetUserByUsername(ctx, username)
}

// GetUserByEmail 根据邮箱获取用户
func (a *StorageAdapter) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return a.userStorage.GetUserByEmail(ctx, email)
}

// UpdateUser 更新用户
func (a *StorageAdapter) UpdateUser(ctx context.Context, id uint, updates map[string]interface{}) error {
	return a.userStorage.UpdateUser(ctx, id, updates)
}

// DeleteUser 删除用户
func (a *StorageAdapter) DeleteUser(ctx context.Context, id uint) error {
	return a.userStorage.DeleteUser(ctx, id)
}

// DeviceStorage 返回设备存储接口
func (a *StorageAdapter) DeviceStorage() DeviceStorage {
	return a.deviceStorage
}

// CreateDevice 创建设备
func (a *StorageAdapter) CreateDevice(ctx context.Context, device *models.Device) error {
	return a.deviceStorage.CreateDevice(ctx, device)
}

// GetDeviceByID 根据ID获取设备
func (a *StorageAdapter) GetDeviceByID(ctx context.Context, id uint) (*models.Device, error) {
	return a.deviceStorage.GetDeviceByID(ctx, id)
}

// GetDeviceByCode 根据设备代码获取设备
func (a *StorageAdapter) GetDeviceByCode(ctx context.Context, deviceCode string) (*models.Device, error) {
	return a.deviceStorage.GetDeviceByCode(ctx, deviceCode)
}

// GetDevicesByUserID 根据用户ID获取设备列表
func (a *StorageAdapter) GetDevicesByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Device, int64, error) {
	return a.deviceStorage.GetDevicesByUserID(ctx, userID, offset, limit)
}

// UpdateDevice 更新设备
func (a *StorageAdapter) UpdateDevice(ctx context.Context, id uint, updates map[string]interface{}) error {
	return a.deviceStorage.UpdateDevice(ctx, id, updates)
}

// DeleteDevice 删除设备
func (a *StorageAdapter) DeleteDevice(ctx context.Context, id uint) error {
	return a.deviceStorage.DeleteDevice(ctx, id)
}

// AgentStorage 返回智能体存储接口
func (a *StorageAdapter) AgentStorage() AgentStorage {
	return a.agentStorage
}

// CreateAgent 创建智能体
func (a *StorageAdapter) CreateAgent(ctx context.Context, agent *models.Agent) error {
	return a.agentStorage.CreateAgent(ctx, agent)
}

// GetAgentByID 根据ID获取智能体
func (a *StorageAdapter) GetAgentByID(ctx context.Context, id uint) (*models.Agent, error) {
	return a.agentStorage.GetAgentByID(ctx, id)
}

// GetAgentsByUserID 根据用户ID获取智能体列表
func (a *StorageAdapter) GetAgentsByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Agent, int64, error) {
	return a.agentStorage.GetAgentsByUserID(ctx, userID, offset, limit)
}

// UpdateAgent 更新智能体
func (a *StorageAdapter) UpdateAgent(ctx context.Context, id uint, updates map[string]interface{}) error {
	return a.agentStorage.UpdateAgent(ctx, id, updates)
}

// DeleteAgent 删除智能体
func (a *StorageAdapter) DeleteAgent(ctx context.Context, id uint) error {
	return a.agentStorage.DeleteAgent(ctx, id)
}

// ConfigStorage 返回配置存储接口
func (a *StorageAdapter) ConfigStorage() ConfigStorage {
	return a.configAdapter
}