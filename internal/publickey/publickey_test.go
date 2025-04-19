package publickey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func generateTestKeys() (privateKey *rsa.PrivateKey, publicKeyPEM []byte, err error) {
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	publicKeyBytes, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	publicKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return privateKey, publicKeyPEM, nil
}

func TestNew(t *testing.T) {
	_, publicKeyPEM, err := generateTestKeys()
	assert.NoError(t, err)

	tempFile, err := os.CreateTemp("", "publickey_test_*.pem")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write(publicKeyPEM)
	assert.NoError(t, err)
	tempFile.Close()

	logger := zap.NewNop()
	pk, err := New(tempFile.Name(), time.Minute, logger)
	assert.NoError(t, err)
	assert.NotNil(t, pk)
	assert.True(t, pk.Enabled())
}

func TestVerify(t *testing.T) {
	privateKey, publicKeyPEM, err := generateTestKeys()
	assert.NoError(t, err)

	tempFile, err := os.CreateTemp("", "publickey_test_*.pem")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write(publicKeyPEM)
	assert.NoError(t, err)
	tempFile.Close()

	logger := zap.NewNop()
	pk, err := New(tempFile.Name(), time.Minute, logger)
	assert.NoError(t, err)

	// Generate a valid token
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "test-subject",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	})
	tokenString, err := token.SignedString(privateKey)
	assert.NoError(t, err)

	subject, err := pk.Verify("Bearer " + tokenString)
	assert.NoError(t, err)
	assert.Equal(t, "test-subject", subject)

	// Test expired token
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "test-subject",
		IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(-time.Minute)),
	})
	expiredTokenString, err := expiredToken.SignedString(privateKey)
	assert.NoError(t, err)

	_, err = pk.Verify("Bearer " + expiredTokenString)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is expired")

	// Test token too old
	oldToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "test-subject",
		IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	})
	oldTokenString, err := oldToken.SignedString(privateKey)
	assert.NoError(t, err)

	_, err = pk.Verify("Bearer " + oldTokenString)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is too old")

	// Test invalid token
	_, err = pk.Verify("Bearer invalid-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is invalid")
}

func TestEnabled(t *testing.T) {
	logger := zap.NewNop()

	// Test with public key file
	_, publicKeyPEM, err := generateTestKeys()
	assert.NoError(t, err)

	tempFile, err := os.CreateTemp("", "publickey_test_*.pem")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write(publicKeyPEM)
	assert.NoError(t, err)
	tempFile.Close()

	pk, err := New(tempFile.Name(), time.Minute, logger)
	assert.NoError(t, err)
	assert.True(t, pk.Enabled())

	// Test without public key file
	pk, err = New("", time.Minute, logger)
	assert.NoError(t, err)
	assert.False(t, pk.Enabled())
}
