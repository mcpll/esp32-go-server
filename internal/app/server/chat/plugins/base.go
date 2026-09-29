package plugins

import "xiaozhi-esp32-server-golang/internal/domain/chat/streamtransform"

// Init registers output transforms.
func Init(registry *streamtransform.Registry) {
	if registry == nil {
		return
	}

	// Register output shaping plugins (text segmentation + tool-call close)
	RegisterOutputSegmenter(registry)
}
