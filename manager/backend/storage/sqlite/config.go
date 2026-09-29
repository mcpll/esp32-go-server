package sqlite

import (
	"fmt"
	"path/filepath"
	"xiaozhi/manager/backend/config"
)

// Config SQLite配置
type Config struct {
	// FilePath 数据库文件路径（如：./data/xiaozhi.db 或 /path/to/xiaozhi.db）
	FilePath string `json:"file_path"`

	// 连接池配置（SQLite 通常单连接足够）
	MaxIdleConns    int `json:"max_idle_conns"`
	MaxOpenConns    int `json:"max_open_conns"`
	ConnMaxLifetime int `json:"conn_max_lifetime"`
}

// NewConfigFromDatabase 从数据库配置创建SQLite配置
func NewConfigFromDatabase(cfg *config.SQLiteConfig) *Config {
	filePath := cfg.FilePath
	if filePath == "" {
		filePath = "./data/xiaozhi.db"
	}

	return &Config{
		FilePath:       filePath,
		MaxIdleConns:   1,
		MaxOpenConns:   1,
		ConnMaxLifetime: 3600,
	}
}

// DSN 生成数据源名称（GORM SQLite 格式）
func (c *Config) DSN() string {
	// 确保使用 file: 前缀以支持更多选项
	return "file:" + c.FilePath + "?_foreign_keys=on&_journal_mode=WAL"
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.FilePath == "" {
		return fmt.Errorf("SQLite file path is required")
	}

	// 检查文件扩展名
	ext := filepath.Ext(c.FilePath)
	if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
		return fmt.Errorf("SQLite file must have .db, .sqlite or .sqlite3 extension")
	}

	return nil
}

// ValidateConfig 验证SQLite配置
func ValidateConfig(cfg *config.SQLiteConfig) error {
	if cfg == nil {
		return fmt.Errorf("SQLite config is required")
	}
	if cfg.FilePath == "" {
		return fmt.Errorf("SQLite file path is required")
	}

	// 检查文件扩展名
	ext := filepath.Ext(cfg.FilePath)
	if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
		return fmt.Errorf("SQLite file must have .db, .sqlite or .sqlite3 extension")
	}

	return nil
}
