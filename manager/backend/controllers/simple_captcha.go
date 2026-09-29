package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

const simpleCaptchaTTL = 5 * time.Minute

type simpleCaptchaChallenge struct {
	Answer    int
	ExpiresAt time.Time
}

type simpleCaptchaStore struct {
	mu         sync.Mutex
	challenges map[string]simpleCaptchaChallenge
}

var authCaptchaStore = &simpleCaptchaStore{
	challenges: make(map[string]simpleCaptchaChallenge),
}

func (s *simpleCaptchaStore) NewChallenge() (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.cleanupLocked(now)

	id, err := randomCaptchaID()
	if err != nil {
		return "", "", err
	}

	left, right, operator, answer, err := generateSimpleMathChallenge()
	if err != nil {
		return "", "", err
	}

	s.challenges[id] = simpleCaptchaChallenge{
		Answer:    answer,
		ExpiresAt: now.Add(simpleCaptchaTTL),
	}

	prompt := fmt.Sprintf("%d %s %d = ?", left, operator, right)
	return id, prompt, nil
}

func (s *simpleCaptchaStore) Verify(id, answer string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.cleanupLocked(now)

	challenge, exists := s.challenges[id]
	if !exists {
		return false
	}

	delete(s.challenges, id)

	parsedAnswer, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil {
		return false
	}

	return challenge.Answer == parsedAnswer
}

func (s *simpleCaptchaStore) cleanupLocked(now time.Time) {
	for id, challenge := range s.challenges {
		if now.After(challenge.ExpiresAt) {
			delete(s.challenges, id)
		}
	}
}

func randomCaptchaID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func generateSimpleMathChallenge() (int, int, string, int, error) {
	left, err := randomIntBetween(2, 10)
	if err != nil {
		return 0, 0, "", 0, err
	}
	right, err := randomIntBetween(1, 9)
	if err != nil {
		return 0, 0, "", 0, err
	}
	operatorSeed, err := randomIntBetween(0, 1)
	if err != nil {
		return 0, 0, "", 0, err
	}

	if operatorSeed == 0 {
		return left, right, "+", left + right, nil
	}

	if right > left {
		left, right = right, left
	}

	return left, right, "-", left - right, nil
}

func randomIntBetween(min, max int) (int, error) {
	if max < min {
		return 0, fmt.Errorf("invalid range: %d-%d", min, max)
	}
	span := max - min + 1
	value, err := rand.Int(rand.Reader, big.NewInt(int64(span)))
	if err != nil {
		return 0, err
	}
	return min + int(value.Int64()), nil
}
