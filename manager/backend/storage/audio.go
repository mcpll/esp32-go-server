package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AudioStorage 音频文件存储工具
type AudioStorage struct {
	BasePath string
	MaxSize  int64
}

// NewAudioStorage 创建音频存储实例
func NewAudioStorage(basePath string, maxSize int64) *AudioStorage {
	// 确保基础目录存在
	if err := os.MkdirAll(basePath, 0755); err != nil {
		panic(fmt.Sprintf("无法创建音频存储目录: %v", err))
	}

	return &AudioStorage{
		BasePath: basePath,
		MaxSize:  maxSize,
	}
}

// SaveAudioFile 保存音频文件
// userID: 用户ID
// groupID: 声纹组ID
// uuid: UUID标识
// fileName: 原始文件名
// fileData: 文件数据
// 返回: 文件保存路径, 文件大小, 错误
func (s *AudioStorage) SaveAudioFile(userID uint, groupID uint, uuid, fileName string, fileData io.Reader) (string, int64, error) {
	// 构建存储路径: storage/speakers/{user_id}/{group_id}/{uuid}.wav
	dirPath := filepath.Join(s.BasePath, fmt.Sprintf("%d", userID), fmt.Sprintf("%d", groupID))

	// 确保目录存在
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", 0, fmt.Errorf("创建目录失败: %v", err)
	}

	// 构建文件路径（使用UUID作为文件名，保留扩展名）
	ext := filepath.Ext(fileName)
	if ext == "" {
		ext = ".wav" // 默认扩展名
	}
	filePath := filepath.Join(dirPath, fmt.Sprintf("%s%s", uuid, ext))

	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("创建文件失败: %v", err)
	}
	defer file.Close()

	// 写入文件数据（限制大小）
	limitedReader := io.LimitReader(fileData, s.MaxSize)
	written, err := io.Copy(file, limitedReader)
	if err != nil {
		os.Remove(filePath) // 删除部分写入的文件
		return "", 0, fmt.Errorf("写入文件失败: %v", err)
	}

	// 检查文件大小
	if written >= s.MaxSize {
		os.Remove(filePath)
		return "", 0, fmt.Errorf("文件大小超过限制: %d 字节", s.MaxSize)
	}

	return filePath, written, nil
}

// SaveVoiceCloneAudioFile 保存复刻音频文件
func (s *AudioStorage) SaveVoiceCloneAudioFile(userID uint, uuid, fileName string, fileData io.Reader) (string, int64, error) {
	dirPath := filepath.Join(s.BasePath, "voice_clones", fmt.Sprintf("%d", userID))
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", 0, fmt.Errorf("创建目录失败: %v", err)
	}

	ext := filepath.Ext(fileName)
	if ext == "" {
		ext = ".wav"
	}
	filePath := filepath.Join(dirPath, fmt.Sprintf("%s%s", uuid, ext))

	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("创建文件失败: %v", err)
	}
	defer file.Close()

	limitedReader := io.LimitReader(fileData, s.MaxSize)
	written, err := io.Copy(file, limitedReader)
	if err != nil {
		os.Remove(filePath)
		return "", 0, fmt.Errorf("写入文件失败: %v", err)
	}
	if written >= s.MaxSize {
		os.Remove(filePath)
		return "", 0, fmt.Errorf("文件大小超过限制: %d 字节", s.MaxSize)
	}

	return filePath, written, nil
}

// DeleteAudioFile 删除音频文件
func (s *AudioStorage) DeleteAudioFile(filePath string) error {
	if filePath == "" {
		return nil
	}

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // 文件不存在，不需要删除
	}

	return os.Remove(filePath)
}

// GetAudioFile 获取音频文件
func (s *AudioStorage) GetAudioFile(filePath string) (*os.File, error) {
	return os.Open(filePath)
}

// FileExists 检查文件是否存在
func (s *AudioStorage) FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
