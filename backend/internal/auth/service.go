package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// NormalizeMobile converts any common Zimbabwean mobile format to +263XXXXXXXXX.
// Handles: +263..., 0..., 263..., or bare 9-digit local number.
func NormalizeMobile(mobile string) string {
	mobile = strings.ReplaceAll(mobile, " ", "")
	mobile = strings.ReplaceAll(mobile, "-", "")
	switch {
	case strings.HasPrefix(mobile, "+263"):
		return mobile
	case strings.HasPrefix(mobile, "263"):
		return "+" + mobile
	case strings.HasPrefix(mobile, "0"):
		return "+263" + mobile[1:]
	default:
		return "+263" + mobile
	}
}

type TokenType string

const (
	TokenAdmin   TokenType = "admin"
	TokenCounter TokenType = "counter"
)

type Claims struct {
	UserID       string    `json:"user_id"`
	TokenType    TokenType `json:"token_type"`
	IsSuperAdmin bool      `json:"is_super_admin,omitempty"`
	jwt.RegisteredClaims
}

type Service struct {
	db        *gorm.DB
	redis     *redis.Client
	jwtSecret []byte
	otpExpiry time.Duration
	otpMaxReq int
}

func NewService(db *gorm.DB, rdb *redis.Client, jwtSecret string, otpExpiryMinutes, otpMaxRequests int) *Service {
	return &Service{
		db:        db,
		redis:     rdb,
		jwtSecret: []byte(jwtSecret),
		otpExpiry: time.Duration(otpExpiryMinutes) * time.Minute,
		otpMaxReq: otpMaxRequests,
	}
}

func (s *Service) IssueToken(userID string, tokenType TokenType, expiryHours int, isSuperAdmin ...bool) (string, error) {
	super := len(isSuperAdmin) > 0 && isSuperAdmin[0]
	claims := Claims{
		UserID:       userID,
		TokenType:    tokenType,
		IsSuperAdmin: super,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func (s *Service) GenerateOTP(ctx context.Context, mobile string) (string, error) {
	rateLimitKey := fmt.Sprintf("otp:rate:%s", mobile)
	count, _ := s.redis.Get(ctx, rateLimitKey).Int()
	if count >= s.otpMaxReq {
		return "", fmt.Errorf("too many OTP requests, please wait")
	}
	otp, err := generateDigits(6)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	s.redis.Set(ctx, fmt.Sprintf("otp:hash:%s", mobile), string(hash), s.otpExpiry)
	s.redis.Incr(ctx, rateLimitKey)
	s.redis.Expire(ctx, rateLimitKey, 15*time.Minute)
	return otp, nil
}

func (s *Service) VerifyOTP(ctx context.Context, mobile, otp string) (bool, error) {
	key := fmt.Sprintf("otp:hash:%s", mobile)
	hash, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, fmt.Errorf("OTP expired or not found")
	}
	if err != nil {
		return false, err
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(otp)); err != nil {
		return false, nil
	}
	s.redis.Del(ctx, key)
	return true, nil
}

func generateDigits(n int) (string, error) {
	result := make([]byte, n)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		result[i] = byte('0') + byte(num.Int64())
	}
	return string(result), nil
}
