package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const ( 
	saltLength = 16
	keyLength = 32
	// Recommended Argon2id parameters (OWASP)
	timeCost = 1
	memoryCost = 64 * 1024 // 64MB
	parallelism = 4
)

// generateSalt generates a random salt
func generateSalt(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// HashPassword hashes a password using Argon2id with a randomly generated salt
func HashPassword(password string) (string, error) {
	salt, err := generateSalt(saltLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, timeCost, memoryCost, parallelism, keyLength)

	// Encode the hash and salt into a single string, including parameters
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memoryCost, timeCost, parallelism, encodedSalt, encodedHash), nil
}

// VerifyPassword verifies a password against its hash
func VerifyPassword(hashedPassword, password string) bool {
	var version int
	var memory, time, parallelism int
	var salt, hash []byte

	_, err := fmt.Sscanf(hashedPassword, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", &version, &memory, &time, &parallelism, &salt, &hash)
	if err != nil {
		return false
	}

	decodedSalt, err := base64.RawStdEncoding.DecodeString(string(salt))
	if err != nil {
		return false
	}
	decodedHash, err := base64.RawStdEncoding.DecodeString(string(hash))
	if err != nil {
		return false
	}

	newHash := argon2.IDKey([]byte(password), decodedSalt, uint32(time), uint32(memory), uint8(parallelism), uint32(keyLength))

	return fmt.Sprintf("%x", newHash) == fmt.Sprintf("%x", decodedHash)
}
