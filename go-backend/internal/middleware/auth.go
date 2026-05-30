package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"xray-panel/internal/auth"
)

func RequireAuth(ss *auth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token := ""
			if strings.HasPrefix(header, "Bearer ") {
				token = header[7:]
			}
			if !ss.Valid(token) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"detail": "未授权或登录已过期"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
