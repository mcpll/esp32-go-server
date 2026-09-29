package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"net"
	"time"
)

type UDPClient struct {
	udpConn    *net.UDPConn
	serverAddr *net.UDPAddr
	aesKey     string
	aesNonce   string
	localSeq   uint32
}

func NewUDPClient(serverAddr string, port int, aesKey, aesNonce string) (*UDPClient, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverAddr, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve UDP address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection: %v", err)
	}

	return &UDPClient{
		udpConn:    conn,
		serverAddr: addr,
		aesKey:     aesKey,
		aesNonce:   aesNonce,
		localSeq:   0,
	}, nil
}

func (c *UDPClient) Close() {
	if c.udpConn != nil {
		c.udpConn.Close()
	}
}

func AesCTREncrypt(key, nonce, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	stream := cipher.NewCTR(block, nonce)
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)
	return ciphertext, nil
}

func AesCTRDecrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	stream := cipher.NewCTR(block, nonce)
	plaintext := make([]byte, len(ciphertext))
	stream.XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

func (c *UDPClient) aesCTREncrypt(key, nonce, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	stream := cipher.NewCTR(block, nonce)
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)
	return ciphertext, nil
}

func (c *UDPClient) aesCTRDecrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	stream := cipher.NewCTR(block, nonce)
	plaintext := make([]byte, len(ciphertext))
	stream.XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

func (c *UDPClient) decryptAudioData(key []byte, data []byte) ([]byte, error) {
	//split nonce and ciphertext
	nonce := data[:16]
	ciphertext := data[16:]

	//decrypt
	decryptedData, err := c.aesCTRDecrypt(key, nonce, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %v", err)
	}

	return decryptedData, nil
}

func (c *UDPClient) SendAudioData(audioData []byte) error {
	// generate a new nonce
	c.localSeq = (c.localSeq + 1) & 0xFFFFFFFF

	// build nonce string: fixed prefix + length + original nonce + sequence
	nonceHex := c.aesNonce[:4] + // fixed prefix (01000000)
		fmt.Sprintf("%04x", len(audioData)) + // data length, 4 hex chars
		c.aesNonce[8:24] + // original nonce
		fmt.Sprintf("%08x", c.localSeq) // sequence number, 8 hex chars

	//fmt.Printf("c.aesNonce: %s len: %d, nonceHex: %s len: %d\n", c.aesNonce, len(c.aesNonce), nonceHex, len(nonceHex))

	// encrypt data
	key, err := hex.DecodeString(c.aesKey)
	if err != nil {
		return fmt.Errorf("failed to decode AES key: %v", err)
	}

	nonceBytes, err := hex.DecodeString(nonceHex)
	if err != nil {
		return fmt.Errorf("failed to decode nonce: %v", err)
	}

	// check IV length
	//fmt.Printf("IV length: %d bytes, content: %x\n", len(nonceBytes), nonceBytes)

	iv := nonceBytes

	encryptedData, err := c.aesCTREncrypt(key, iv, audioData)
	if err != nil {
		return fmt.Errorf("failed to encrypt data: %v", err)
	}

	// concatenate nonce and ciphertext
	packet := append(nonceBytes, encryptedData...)

	// send the packet
	_, err = c.udpConn.Write(packet)
	if err != nil {
		return fmt.Errorf("failed to send UDP packet: %v", err)
	}
	markUDPTraffic()

	//fmt.Printf("send data: nonce=%s, seq=%d, dataLen=%d\n", nonceHex, c.localSeq, len(audioData))

	return nil
}

func (c *UDPClient) ReceiveAudioData(key []byte, cb func(key []byte, data []byte)) error {
	go func() {
		for {
			buffer := make([]byte, 1024)
			n, _, err := c.udpConn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Println(err)
				return
			}

			if !firstAudio {
				firstAudio = true
				fmt.Printf("收到第一条音频消息, 耗时: %d ms\n", time.Now().UnixMilli()-sendAudioEndTs)
			}

			cb(key, buffer[:n])
		}
	}()

	return nil
}
