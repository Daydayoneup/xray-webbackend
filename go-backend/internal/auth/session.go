package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type SessionStore struct {
	mu     sync.Mutex
	ttl    int64
	tokens map[string]int64
}

func NewSessionStore(ttl int) *SessionStore {
	return &SessionStore{
		ttl:    int64(ttl),
		tokens: map[string]int64{},
	}
}

func (s *SessionStore) Create() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	for tok, exp := range s.tokens {
		if exp <= now {
			delete(s.tokens, tok)
		}
	}
	b := make([]byte, 24)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.tokens[tok] = now + s.ttl
	return tok
}

func (s *SessionStore) Valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	if ok && exp > time.Now().Unix() {
		return true
	}
	delete(s.tokens, token)
	return false
}

func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = map[string]int64{}
}
