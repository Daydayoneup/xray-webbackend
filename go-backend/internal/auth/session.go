package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type SessionStore struct {
	ttl    int64
	mu     sync.RWMutex
	tokens map[string]int64
}

func NewSessionStore(ttl int64) *SessionStore {
	return &SessionStore{ttl: ttl, tokens: make(map[string]int64)}
}

func (s *SessionStore) Create() string {
	b := make([]byte, 24)
	rand.Read(b)
	token := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	for t, exp := range s.tokens {
		if exp <= now {
			delete(s.tokens, t)
		}
	}
	s.tokens[token] = now + s.ttl
	return token
}

func (s *SessionStore) Valid(token string) bool {
	s.mu.RLock()
	exp, ok := s.tokens[token]
	s.mu.RUnlock()
	if !ok || exp <= time.Now().Unix() {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

func (s *SessionStore) Clear() {
	s.mu.Lock()
	s.tokens = make(map[string]int64)
	s.mu.Unlock()
}
