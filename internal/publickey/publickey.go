package publickey

import (
	"crypto/rsa"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Publickey struct
type Publickey struct {
	publicKeyFile string
	verifyKey     *rsa.PublicKey
	freshnessTime time.Duration
}

// New publickey reader/checker
func New(publicKeyFile string, freshnessTime time.Duration, logger *zap.Logger) (*Publickey, error) {
	var verifyKey *rsa.PublicKey
	if publicKeyFile != "" {
		verifyBytes, err := os.ReadFile(publicKeyFile)
		if err != nil {
			return nil, errors.Wrap(err, "failed read pubkey")
		}
		verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
		if err != nil {
			return nil, errors.Wrap(err, "failed parse pubkey")
		}
	}
	return &Publickey{
		publicKeyFile: publicKeyFile,
		verifyKey:     verifyKey,
		freshnessTime: freshnessTime,
	}, nil
}

// Enabled publickey is enabled
func (pk Publickey) Enabled() bool {
	return pk.publicKeyFile != ""
}

// Verify verify auth header
func (pk Publickey) Verify(t string) (string, error) {
	if t == "" {
		return "", fmt.Errorf("no tokenString")
	}
	t = strings.TrimPrefix(t, "Bearer ")

	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(t, claims, func(token *jwt.Token) (interface{}, error) {
		return pk.verifyKey, nil
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}))

	if err != nil {
		return "", fmt.Errorf("token is invalid: %v", err)
	}

	now := time.Now()
	iat := now.Add(-pk.freshnessTime)

	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(now) {
		return "", fmt.Errorf("token is expired")
	}
	if claims.IssuedAt == nil || claims.IssuedAt.Time.Before(iat) {
		return "", fmt.Errorf("token is too old")
	}

	return claims.Subject, nil
}
