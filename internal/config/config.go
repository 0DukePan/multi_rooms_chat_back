package config

import (
	"os"
)

type Config struct {
	Port            string `env:"PORT"`
	LogLevel        string `env:"LOG_LEVEL"`
	DatabaseURL     string `env:"DATABASE_URL,secret"`
	RedisURL        string `env:"REDIS_URL,secret"`
	JWTSecret       string `env:"JWT_SECRET,secret"`
	JWTPrivateKey   string `env:"JWT_PRIVATE_KEY,secret"`
	JWTPublicKey    string `env:"JWT_PUBLIC_KEY,secret"`
	FileStoragePath string `env:"FILE_STORAGE_PATH"`
	BaseFileURL     string `env:"BASE_FILE_URL"`
	ClamAVAddress   string `env:"CLAMAV_ADDRESS"`
	ClamAVTimeout   string `env:"CLAMAV_TIMEOUT"`
}

// Load loads configuration from environment variables
func Load() *Config {
	cfg := &Config{
		Port:            getEnv("PORT", "8080"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		RedisURL:        getEnv("REDIS_URL", ""),
		JWTSecret:       getEnv("JWT_SECRET", ""),
		JWTPrivateKey:   getEnv("JWT_PRIVATE_KEY", ""),
		JWTPublicKey:    getEnv("JWT_PUBLIC_KEY", ""),
		FileStoragePath: getEnv("FILE_STORAGE_PATH", "./data/uploads"),
		BaseFileURL:     getEnv("BASE_FILE_URL", "/files"),
		ClamAVAddress:   getEnv("CLAMAV_ADDRESS", ""),
		ClamAVTimeout:   getEnv("CLAMAV_TIMEOUT", "5s"),
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
