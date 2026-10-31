package auth

import (
	"testing"
	"time"
)

func TestSessionCreateAndValidate(t *testing.T) {
	ss := NewSessionStore(3600)
	token := ss.Create()
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if !ss.Valid(token) {
		t.Error("newly created token should be valid")
	}
	if ss.Valid("fake-token") {
		t.Error("fake token should not be valid")
	}
}

func TestSessionRevoke(t *testing.T) {
	ss := NewSessionStore(3600)
	token := ss.Create()
	ss.Revoke(token)
	if ss.Valid(token) {
		t.Error("revoked token should not be valid")
	}
}

func TestSessionClear(t *testing.T) {
	ss := NewSessionStore(3600)
	t1 := ss.Create()
	t2 := ss.Create()
	ss.Clear()
	if ss.Valid(t1) || ss.Valid(t2) {
		t.Error("cleared tokens should not be valid")
	}
}

func TestSessionExpiry(t *testing.T) {
	ss := NewSessionStore(-1)
	token := ss.Create()
	time.Sleep(10 * time.Millisecond)
	if ss.Valid(token) {
		t.Error("expired token should not be valid")
	}
}
