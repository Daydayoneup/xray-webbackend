package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"

	"golang.org/x/crypto/pbkdf2"

	"xray-panel/internal/model"
)

const pbkdf2Iterations = 200_000

func HashPassword(pw string, salt ...string) (*model.PasswordRec, error) {
	s := ""
	if len(salt) > 0 {
		s = salt[0]
	} else {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s = hex.EncodeToString(b)
	}
	saltBytes, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	h := pbkdf2.Key([]byte(pw), saltBytes, pbkdf2Iterations, sha256.Size, sha256.New)
	return &model.PasswordRec{Salt: s, Hash: hex.EncodeToString(h)}, nil
}

func VerifyPassword(rec *model.PasswordRec, pw string) bool {
	if rec == nil {
		return false
	}
	calc, err := HashPassword(pw, rec.Salt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(calc.Hash), []byte(rec.Hash)) == 1
}
