package mqtt_udp

import (
	"crypto/aes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	. "xiaozhi-esp32-server-golang/logger"
)

// UDPServer UDP server
/*
type UDPServer struct {
	conn       *net.UDPConn
	sessions   map[string]*Session
	mqttServer *MqttServer
	udpPort    int
	sync.RWMutex
}*/

type UdpServer struct {
	conn           *net.UDPConn
	udpPort        int      //udp server listen port
	externalHost   string   //udp server external host
	externalPort   int      //udp server external port
	connId2Session sync.Map //connId => UdpSession
	mqttAdapter    *MqttUdpAdapter
	sync.RWMutex
}

const maxConnIDGenerateAttempts = 16

var udpRandReader io.Reader = rand.Reader

// NewUDPServer creates a UDP server
func NewUDPServer(udpPort int, externalHost string, externalPort int) *UdpServer {
	return &UdpServer{
		udpPort:        udpPort,
		externalHost:   externalHost,
		externalPort:   externalPort,
		connId2Session: sync.Map{},
	}
}

// Start starts the UDP server
func (s *UdpServer) Start() error {
	addr := &net.UDPAddr{
		IP:   net.ParseIP("0.0.0.0"),
		Port: s.udpPort,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("监听UDP失败: %v", err)
	}

	s.conn = conn
	Infof("UDP服务器启动在 %s:%d", "0.0.0.0", s.udpPort)

	// Start session cleanup
	//go s.cleanupSessions()

	// Start packet processing
	go s.handlePackets()

	return nil
}

// Close shuts down the UDP server so handlePackets exits
func (s *UdpServer) Close() error {
	s.Lock()
	conn := s.conn
	s.conn = nil
	s.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// handlePackets handles received packets
func (s *UdpServer) handlePackets() {
	buffer := make([]byte, 4096) // default buffer size
	for {
		s.RLock()
		conn := s.conn
		s.RUnlock()
		if conn == nil {
			return
		}
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			s.RLock()
			closed := s.conn == nil
			s.RUnlock()
			if closed {
				return
			}
			Errorf("读取UDP数据失败: %v", err)
			continue
		}

		// Copy data to avoid concurrent mutation
		data := make([]byte, n)
		copy(data, buffer[:n])

		// Process packet
		s.processPacket(addr, data)
	}
}

func (s *UdpServer) getSessionByConnID(connID string) *UdpSession {
	val, ok := s.connId2Session.Load(connID)
	if ok {
		return val.(*UdpSession)
	}
	return nil
}

// processPacket handles a single packet
func (s *UdpServer) processPacket(addr *net.UDPAddr, data []byte) {
	// Check packet size
	if len(data) < 16 {
		Warn("数据包太小")
		return
	}

	fullNonce := data[:16]
	connID := fullNonce[4:8] // bytes 5-8 as connection id
	strConnID := hex.EncodeToString(connID)
	udpSession := s.getSessionByConnID(strConnID)
	if udpSession == nil {
		//Warnf("session not found addr: %s, connID: %s", addr, strConnID)
		return
	}

	// Update last activity time
	udpSession.LastActive = time.Now()

	decrypted, err := udpSession.Decrypt(data)
	if err != nil {
		Errorf("addr: %s 解密失败: %v", addr, err)
		return
	}
	currentAddr := udpSession.GetRemoteAddr()
	if currentAddr == nil || currentAddr.String() != addr.String() {
		udpSession.SetRemoteAddr(addr)
	}
	Debugf("收到音频数据, addr: %s, 大小: %d 字节", addr, len(decrypted))
	ok, err := udpSession.RecvData(decrypted)
	if err != nil {
		Errorf("addr: %s 接收数据失败: %v", addr, err)
		return
	}
	if !ok {
		Warnf("addr: %s 接收数据失败, 通道已满", addr)
		return
	}
	/*select {
	case udpSession.RecvChannel <- decrypted:
		return
	default:
		Warnf("udpSession.RecvChannel is full, addr: %s", addr)
	}*/
}

// cleanupSessions removes expired sessions
func (s *UdpServer) cleanupSessions() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		now := time.Now()
		s.connId2Session.Range(func(key, value interface{}) bool {
			session := value.(*UdpSession)
			if now.Sub(session.LastActive) > 5*time.Minute {
				s.connId2Session.Delete(key)
				Infof("清理过期会话: %s", key)
			}
			return true
		})
	}
}

// CreateSession creates a new session
func (s *UdpServer) CreateSession(deviceId, clientId string) *UdpSession {
	// Generate session ID
	sessionID, err := generateSessionID()
	if err != nil {
		Errorf("生成会话ID失败: %v", err)
		return nil
	}

	// Generate AES key
	key := make([]byte, 16)
	if err := fillRandomBytes(key); err != nil {
		Errorf("生成AES密钥失败: %v", err)
		return nil
	}

	// Create AES block
	block, err := aes.NewCipher(key)
	if err != nil {
		Errorf("创建AES块失败: %v", err)
		return nil
	}

	// Convert key to [16]byte
	aesKey := [16]byte{}
	copy(aesKey[:], key)

	for attempt := 0; attempt < maxConnIDGenerateAttempts; attempt++ {
		// Generate 4-byte connection id
		connID := make([]byte, 4)
		if err := fillRandomBytes(connID); err != nil {
			Errorf("生成连接ID失败: %v", err)
			return nil
		}
		strConnID := hex.EncodeToString(connID)

		// 4-byte timestamp
		timestamp := make([]byte, 4)
		binary.BigEndian.PutUint32(timestamp, uint32(time.Now().Unix()))

		// Build nonce: 4-byte conn id + 4-byte timestamp
		nonce := append(connID, timestamp...)

		// Convert nonce to [8]byte
		nonceBytes := [8]byte{}
		copy(nonceBytes[:], nonce)

		// Create session
		session := &UdpSession{
			ID:          sessionID,
			ConnId:      strConnID,
			ClientId:    clientId,
			DeviceId:    deviceId,
			AesKey:      aesKey,
			Nonce:       nonceBytes, // store original nonce template
			CreatedAt:   time.Now(),
			LastActive:  time.Now(),
			Block:       block,
			RecvChannel: make(chan []byte, 100),
			SendChannel: make(chan []byte, 100),
			Status:      UdpSessionStatusActive,
			Lock:        sync.Mutex{},
		}

		if _, loaded := s.connId2Session.LoadOrStore(strConnID, session); loaded {
			Warnf("UDP connID冲突，重试生成: device=%s, connID=%s, attempt=%d", deviceId, strConnID, attempt+1)
			continue
		}

		s.startSessionSender(session)
		return session
	}

	Errorf("生成唯一UDP connID失败: device=%s", deviceId)
	return nil
}

func (s *UdpServer) startSessionSender(session *UdpSession) {
	go func() {
		for data := range session.SendChannel {
			remoteAddr := session.WaitRemoteAddr(2 * time.Second)
			if remoteAddr == nil {
				dropped := 1 + session.DrainPendingAudio()
				Warnf("UDP远端地址未建立，TTS音频被丢弃: device=%s, connId=%s, dropped=%d", session.DeviceId, session.ConnId, dropped)
				continue
			}
			encrypted, err := session.Encrypt(data)
			if err != nil {
				Errorf("加密失败: %v", err)
				continue
			}
			_, err = s.writeToUDP(encrypted, remoteAddr)
			if err != nil {
				Errorf("发送音频数据失败: %v", err)
				continue
			}
		}
	}()
}

func (s *UdpServer) writeToUDP(data []byte, remoteAddr *net.UDPAddr) (int, error) {
	s.RLock()
	conn := s.conn
	s.RUnlock()
	if conn == nil {
		return 0, fmt.Errorf("udp server is closed")
	}
	return conn.WriteToUDP(data, remoteAddr)
}

// CloseSession closes a session
func (s *UdpServer) CloseSession(connID string) {
	session := s.getSessionByConnID(connID)
	s.CloseSessionByRef(session)
}

// ClearSessionAddrBinding clears UDP addr binding for connID without destroying the session
func (s *UdpServer) ClearSessionAddrBinding(connID string) {
	session := s.getSessionByConnID(connID)
	if session == nil {
		return
	}
	session.SetRemoteAddr(nil)
}

func (s *UdpServer) SetConnId2Session(connID string, session *UdpSession) {
	Debugf("SetConnId2Session, connID: %s, session: %+v", connID, session)
	s.connId2Session.Store(connID, session)
}

// GetSessionByConnID returns session info
func (s *UdpServer) GetSessionByConnID(connID string) *UdpSession {
	val, ok := s.connId2Session.Load(connID)
	if ok {
		return val.(*UdpSession)
	}
	return nil
}

// generateSessionID generates a session ID
func generateSessionID() (string, error) {
	b := make([]byte, 8)
	if err := fillRandomBytes(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func fillRandomBytes(buffer []byte) error {
	_, err := io.ReadFull(udpRandReader, buffer)
	return err
}

func (s *UdpServer) CloseSessionByRef(session *UdpSession) {
	if session == nil {
		return
	}
	s.connId2Session.Delete(session.ConnId)
	session.SetRemoteAddr(nil)
	session.Destroy()
}
