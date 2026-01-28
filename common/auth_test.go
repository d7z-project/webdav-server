package common

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/argon2"
)

func TestVerifyPassword(t *testing.T) {
	// Plain text
	assert.True(t, verifyPassword("password", "password"))
	assert.False(t, verifyPassword("password", "wrong"))

	// SHA-256
	// echo -n "password" | sha256sum
	sha256Hash := "sha256:5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"
	assert.True(t, verifyPassword(sha256Hash, "password"))
	assert.False(t, verifyPassword(sha256Hash, "wrong"))

	// Argon2id
	// We use a known good hash if possible, or generate one here for testing
	// $argon2id$v=19$m=16,t=2,p=1$c2FsdHNhbHQ$i8VInoFfTDCb/C/8R5xJpA was my manual attempt
	
	// Let's generate one to be sure
	salt := []byte("saltsalt")
	hash := argon2.IDKey([]byte("password"), salt, 2, 16, 1, 16)
	generatedArgon2id := fmt.Sprintf("argon2id:$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, 16, 2, 1,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
	
	assert.True(t, verifyPassword(generatedArgon2id, "password"))
	assert.False(t, verifyPassword(generatedArgon2id, "wrong"))

	// Invalid Argon2id format
	assert.False(t, verifyPassword("argon2id:invalid", "password"))
}
