package storage

import (
	"fmt"

	"xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/storage/mysql"
	"xiaozhi/manager/backend/storage/sqlite"
)

// StorageType 存储类型
type StorageType string

const (
	StorageTypeMySQL  StorageType = "mysql"
	StorageTypeSQLite StorageType = "sqlite"
)

// Factory 存储工厂
type Factory struct{}

// NewFactory 创建存储工厂
func NewFactory() *Factory {
	return &Factory{}
}

// CreateStorage 创建存储实例
func CreateStorage(dbConfig config.DatabaseConfig) (*StorageAdapter, error) {
	// 根据配置判断存储类型
	storageType := dbConfig.GetStorageType()

	switch StorageType(storageType) {
	case StorageTypeSQLite:
		if dbConfig.SQLite == nil {
			return nil, fmt.Errorf("SQLite config is required")
		}
		// 验证SQLite配置
		if err := sqlite.ValidateConfig(dbConfig.SQLite); err != nil {
			return nil, fmt.Errorf("invalid SQLite config: %w", err)
		}
		// 创建SQLite配置
		sqliteConfig := sqlite.NewConfigFromDatabase(dbConfig.SQLite)
		// 创建SQLite存储
		sqliteStorage, err := sqlite.NewStorage(sqliteConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create SQLite storage: %w", err)
		}
		// 创建基础存储
		baseStorage := NewGormBaseStorage(sqliteStorage.DB)
		// 返回适配器
		return NewStorageAdapter(baseStorage), nil

	case StorageTypeMySQL:
		if dbConfig.MySQL == nil {
			return nil, fmt.Errorf("MySQL config is required")
		}
		// 验证MySQL配置
		if err := mysql.ValidateConfig(dbConfig.MySQL); err != nil {
			return nil, fmt.Errorf("invalid MySQL config: %w", err)
		}
		// 创建MySQL配置
		mysqlConfig := mysql.NewConfigFromDatabase(dbConfig.MySQL)
		// 创建MySQL存储
		mysqlStorage, err := mysql.NewStorage(mysqlConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create MySQL storage: %w", err)
		}
		// 创建基础存储
		baseStorage := NewGormBaseStorage(mysqlStorage.DB)
		// 返回适配器
		return NewStorageAdapter(baseStorage), nil

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}

// GetSupportedTypes 获取支持的存储类型
func (f *Factory) GetSupportedTypes() []StorageType {
	return []StorageType{
		StorageTypeMySQL,
		StorageTypeSQLite,
	}
}
