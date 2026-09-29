package websocket

import (
	"io"
	"net/http"
	"strings"
	"xiaozhi-esp32-server-golang/internal/app/server/chat"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

// handleVisionAPI handles the image-recognition API
func (s *WebSocketServer) handleVisionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Warnf("vision request method not allowed: %s", r.Method)
		http.Error(w, "仅支持POST请求", http.StatusMethodNotAllowed)
		return
	}

	//read Device-Id and Client-Id from headers
	deviceId := r.Header.Get("Device-Id")
	clientId := r.Header.Get("Client-Id")
	_ = clientId
	if deviceId == "" {
		log.Errorf("vision request missing Device-Id")
		http.Error(w, "缺少Device-Id", http.StatusBadRequest)
		return
	}
	log.Infof("vision request deviceId=%s", deviceId)

	if viper.GetBool("vision.enable_auth") {

		//get Bearer token from the Authorization header
		authToken := r.Header.Get("Authorization")
		if authToken == "" {
			log.Errorf("vision request missing Authorization deviceId=%s", deviceId)
			http.Error(w, "缺少Authorization", http.StatusBadRequest)
			return
		}
		authToken = strings.TrimPrefix(authToken, "Bearer ")

		err := chat.VisvionAuth(authToken)
		if err != nil {
			log.Errorf("vision auth failed deviceId=%s err=%v", deviceId, err)
			http.Error(w, "图片识别认证失败", http.StatusUnauthorized)
			return
		}
		log.Infof("vision auth passed deviceId=%s", deviceId)
	}

	// parse multipart form, max 10MB
	question := r.FormValue("question")
	if question == "" {
		log.Warnf("vision request missing question deviceId=%s", deviceId)
		http.Error(w, "缺少question参数", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		log.Errorf("vision request missing file or read failed deviceId=%s err=%v", deviceId, err)
		http.Error(w, "缺少file参数或文件读取失败", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Errorf("vision file read failed deviceId=%s err=%v", deviceId, err)
		http.Error(w, "文件读取失败", http.StatusInternalServerError)
		return
	}

	file.Close()
	log.Infof("vision received file deviceId=%s filename=%s size=%d question=%s", deviceId, header.Filename, len(fileBytes), question)

	result, err := chat.HandleVllm(deviceId, fileBytes, question)
	if err != nil {
		log.Errorf("vision recognition failed deviceId=%s err=%v", deviceId, err)
		http.Error(w, "图片识别失败", http.StatusInternalServerError)
		return
	}

	log.Infof("vision recognition succeeded deviceId=%s resultLen=%d", deviceId, len(result))
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(result))
}
