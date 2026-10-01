package play_music

import (
	"context"
)

// MusicPlayerInterface music player interface
type MusicPlayerInterface interface {
	// PlayMusicStream plays music from a URL and returns an audio stream channel
	PlayMusicStream(ctx context.Context, url string) (chan []byte, error)

	// GetPlayerInfo returns player info
	GetPlayerInfo() map[string]interface{}

	// Stop stops the player
	Stop() error
}

// MusicPlayerConfig music player config
type MusicPlayerConfig struct {
	FrameDuration int    `json:"frame_duration"` // frame duration (ms), default 20ms
	AudioFormat   string `json:"audio_format"`   // audio format, default "mp3"
}

// DefaultMusicPlayerConfig default music player config
func DefaultMusicPlayerConfig() *MusicPlayerConfig {
	return &MusicPlayerConfig{
		FrameDuration: 20,    // 20ms
		AudioFormat:   "mp3", // MP3 format
	}
}

// ToMap converts config to a map
func (c *MusicPlayerConfig) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"frame_duration": c.FrameDuration,
		"audio_format":   c.AudioFormat,
	}
}

// AudioStreamInfo audio stream info
type AudioStreamInfo struct {
	URL           string `json:"url"`
	Format        string `json:"format"`         // audio format, e.g. "mp3", "wav"
	SampleRate    int    `json:"sample_rate"`    // sample rate
	Channels      int    `json:"channels"`       // channel count
	Duration      int64  `json:"duration"`       // duration (ms)
	ContentLength int64  `json:"content_length"` // content length (bytes)
}

// PlaybackStatus playback status
type PlaybackStatus int

const (
	StatusIdle PlaybackStatus = iota
	StatusPlaying
	StatusPaused
	StatusStopped
	StatusError
)

// String returns the string form of the status
func (s PlaybackStatus) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusPlaying:
		return "playing"
	case StatusPaused:
		return "paused"
	case StatusStopped:
		return "stopped"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// PlaybackEvent playback event
type PlaybackEvent struct {
	Type      string      `json:"type"`      // event type: "started", "progress", "finished", "error"
	Timestamp int64       `json:"timestamp"` // timestamp
	Message   string      `json:"message"`   // event message
	Data      interface{} `json:"data"`      // extra data
}

// StreamingStats streaming playback stats
type StreamingStats struct {
	BytesDownloaded int64          `json:"bytes_downloaded"` // bytes downloaded
	BytesDecoded    int64          `json:"bytes_decoded"`    // bytes decoded
	FramesGenerated int64          `json:"frames_generated"` // frames generated
	StartTime       int64          `json:"start_time"`       // start time
	FirstFrameTime  int64          `json:"first_frame_time"` // first-frame time
	Status          PlaybackStatus `json:"status"`           // current status
	ErrorCount      int            `json:"error_count"`      // error count
}
