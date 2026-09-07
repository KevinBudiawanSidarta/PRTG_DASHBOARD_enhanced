package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	OrgID string `json:"org_id"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

func OrgFromRequest(r *http.Request) (Claims, error) {
	if os.Getenv("DEV_AUTH") == "true" {
		org := r.Header.Get("X-Organization-ID")
		if org == "" {
			return Claims{}, fmt.Errorf("X-Organization-ID required in DEV_AUTH")
		}
		return Claims{OrgID: org, Role: "admin"}, nil
	}
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if tok == "" {
		return Claims{}, fmt.Errorf("missing bearer token")
	}
	secret := os.Getenv("SUPABASE_JWT_SECRET")
	if secret == "" {
		return Claims{}, fmt.Errorf("SUPABASE_JWT_SECRET is required")
	}
	claims := Claims{}
	parsed, err := jwt.ParseWithClaims(tok, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		return Claims{}, fmt.Errorf("invalid token: %w", err)
	}
	if claims.OrgID == "" {
		return Claims{}, fmt.Errorf("org_id claim missing")
	}
	return claims, nil
}
func WithOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := OrgFromRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKey struct{}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}
