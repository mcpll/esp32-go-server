package storage

import (
	"context"
	"xiaozhi/manager/backend/models"
)

// Storage 通用存储接口
type Storage interface {
	// 连接管理
	Connect(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error
	
	// 事务管理
	BeginTx(ctx context.Context) (Transaction, error)
	
	// 用户管理
	UserStorage
	// 设备管理
	DeviceStorage
	// 智能体管理
	AgentStorage
	// 配置管理
	ConfigStorage
}

// Transaction 事务接口
type Transaction interface {
	Commit() error
	Rollback() error
	// 在事务中执行存储操作
	UserStorage
	DeviceStorage
	AgentStorage
	ConfigStorage
}

// UserStorage 用户存储接口
type UserStorage interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByID(ctx context.Context, id uint) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUsers(ctx context.Context, offset, limit int) ([]*models.User, int64, error)
	UpdateUser(ctx context.Context, id uint, updates map[string]interface{}) error
	DeleteUser(ctx context.Context, id uint) error
}

// DeviceStorage 设备存储接口
type DeviceStorage interface {
	CreateDevice(ctx context.Context, device *models.Device) error
	GetDeviceByID(ctx context.Context, id uint) (*models.Device, error)
	GetDeviceByCode(ctx context.Context, deviceCode string) (*models.Device, error)
	GetDevicesByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Device, int64, error)
	UpdateDevice(ctx context.Context, id uint, updates map[string]interface{}) error
	DeleteDevice(ctx context.Context, id uint) error
}

// AgentStorage 智能体存储接口
type AgentStorage interface {
	CreateAgent(ctx context.Context, agent *models.Agent) error
	GetAgentByID(ctx context.Context, id uint) (*models.Agent, error)
	GetAgentsByUserID(ctx context.Context, userID uint, offset, limit int) ([]*models.Agent, int64, error)
	UpdateAgent(ctx context.Context, id uint, updates map[string]interface{}) error
	DeleteAgent(ctx context.Context, id uint) error
}

// ConfigStorage 配置存储接口
type ConfigStorage interface {
	// 通用配置操作
	CreateConfig(ctx context.Context, config *models.Config) error
	GetConfigs(ctx context.Context, configType string) ([]*models.Config, error)
	GetConfigByID(ctx context.Context, id uint) (*models.Config, error)
	GetConfigByTypeAndName(ctx context.Context, configType, name string) (*models.Config, error)
	GetDefaultConfig(ctx context.Context, configType string) (*models.Config, error)
	UpdateConfig(ctx context.Context, id uint, updates map[string]interface{}) error
	DeleteConfig(ctx context.Context, id uint) error
	SetDefaultConfig(ctx context.Context, configType string, id uint) error
	
	// 全局角色配置
	CreateGlobalRole(ctx context.Context, role *models.GlobalRole) error
	GetGlobalRoles(ctx context.Context) ([]*models.GlobalRole, error)
	GetGlobalRoleByID(ctx context.Context, id uint) (*models.GlobalRole, error)
	UpdateGlobalRole(ctx context.Context, id uint, updates map[string]interface{}) error
	DeleteGlobalRole(ctx context.Context, id uint) error
}

// StorageConfig 存储配置接口
type StorageConfig interface {
	GetType() string
	Validate() error
}