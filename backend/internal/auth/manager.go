package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID    uint64 `json:"user_id"`
	Username  string `json:"username"`
	SpaceID   string `json:"space_id"`
	SpaceRole string `json:"space_role"`
	IsAdmin   bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, minutes int) *Manager {
	if strings.TrimSpace(secret) == "" {
		secret = ephemeralSecret()
	}
	if minutes <= 0 {
		minutes = 720
	}
	return &Manager{secret: []byte(secret), ttl: time.Duration(minutes) * time.Minute}
}

func ephemeralSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err == nil {
		return base64.RawURLEncoding.EncodeToString(bytes)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func (m *Manager) Issue(user domain.User, spaceID, role string) (string, time.Time, error) {
	now := time.Now().UTC()
	expires := now.Add(m.ttl)
	claims := Claims{UserID: user.ID, Username: user.Username, SpaceID: spaceID, SpaceRole: role, IsAdmin: user.IsSuperAdmin, RegisteredClaims: jwt.RegisteredClaims{Subject: strconv.FormatUint(user.ID, 10), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	return signed, expires, err
}

func (m *Manager) Parse(raw string) (Claims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Claims{}, ErrInvalidToken
	}
	parsed, err := jwt.ParseWithClaims(raw, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil || parsed == nil || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || claims.UserID == 0 {
		return Claims{}, ErrInvalidToken
	}
	return *claims, nil
}
