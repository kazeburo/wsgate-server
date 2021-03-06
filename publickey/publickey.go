package publickey

import (
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
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
		verifyBytes, err := ioutil.ReadFile(publicKeyFile)
		if err != nil {
			return nil, errors.Wrap(err, "Failed read pubkey")
		}
		verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
		if err != nil {
			return nil, errors.Wrap(err, "Failed parse pubkey")
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
	claims := &jwt.StandardClaims{}
	jwp := &jwt.Parser{
		ValidMethods:         []string{"RS256", "RS384", "RS512"},
		SkipClaimsValidation: false,
	}
	_, err := jwp.ParseWithClaims(t, claims, func(token *jwt.Token) (interface{}, error) {
		return pk.verifyKey, nil
	})

	if err != nil {
		return "", fmt.Errorf("Token is invalid: %v", err)
	}

	now := time.Now()
	iat := now.Add(-pk.freshnessTime)
	if claims.ExpiresAt == 0 || claims.ExpiresAt < now.Unix() {
		return "", fmt.Errorf("Token is expired")
	}
	if claims.IssuedAt == 0 || claims.IssuedAt < iat.Unix() {
		return "", fmt.Errorf("Token is too old")
	}

	return claims.Subject, nil
}
