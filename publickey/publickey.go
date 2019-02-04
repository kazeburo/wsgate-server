package publickey

import (
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"strings"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Publickey struct
type Publickey struct {
	publicKeyFile string
	verifyKey     *rsa.PublicKey
}

// New publickey reader/checker
func New(publicKeyFile string, logger *zap.Logger) (*Publickey, error) {
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
	}, nil
}

// Enabled publickey is enabled
func (pk Publickey) Enabled() bool {
	return pk.publicKeyFile != ""
}

// Verify verify auth header
func (pk Publickey) Verify(t string) (bool, error) {
	if t == "" {
		return false, fmt.Errorf("no tokenString")
	}
	t = strings.TrimPrefix(t, "Bearer ")
	claims := &jwt.StandardClaims{}
	token, err := jwt.ParseWithClaims(t, claims, func(token *jwt.Token) (interface{}, error) {
		return pk.verifyKey, nil
	})

	if err != nil {
		return false, fmt.Errorf("Token is invalid: %v", err)
	}
	if !token.Valid {
		return false, fmt.Errorf("Token is invalid")
	}
	if claims.Valid() != nil {
		return false, fmt.Errorf("Invalid claims: %v", claims.Valid())
	}
	return true, nil
}
