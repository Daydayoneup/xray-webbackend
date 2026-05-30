package auth

import "testing"

func TestHashPassword(t *testing.T) {
	rec, err := HashPassword("testpw")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Salt == "" {
		t.Error("salt should not be empty")
	}
	if rec.Hash == "" {
		t.Error("hash should not be empty")
	}
	if len(rec.Salt) != 32 {
		t.Errorf("salt length = %d, want 32", len(rec.Salt))
	}
	if len(rec.Hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(rec.Hash))
	}
}

func TestHashPasswordDeterministic(t *testing.T) {
	salt := "aabbccddeeff00112233445566778899"
	rec1, _ := HashPassword("testpw", salt)
	rec2, _ := HashPassword("testpw", salt)
	if rec1.Hash != rec2.Hash {
		t.Error("same password + same salt should produce same hash")
	}
}

func TestVerifyPassword(t *testing.T) {
	rec, _ := HashPassword("correct")
	if !VerifyPassword(rec, "correct") {
		t.Error("correct password should verify")
	}
	if VerifyPassword(rec, "wrong") {
		t.Error("wrong password should not verify")
	}
	if VerifyPassword(nil, "anything") {
		t.Error("nil record should not verify")
	}
}
