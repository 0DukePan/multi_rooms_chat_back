package config

import (
	"os"
	"strconv"
)

type Config struct {
	Environment          string `env:"ENVIRONMENT"`
	Port                 string `env:"PORT"`
	LogLevel             string `env:"LOG_LEVEL"`
	DatabaseURL          string `env:"DATABASE_URL,secret"`
	RedisURL             string `env:"REDIS_URL"`
	RedisPassword        string `env:"REDIS_PASSWORD,secret"`
	RedisDB              int    `env:"REDIS_DB"`
	RedisPoolMaxIdle     int    `env:"REDIS_POOL_MAX_IDLE"`
	RedisPoolMaxActive   int    `env:"REDIS_POOL_MAX_ACTIVE"`
	RedisPoolIdleTimeout string `env:"REDIS_POOL_IDLE_TIMEOUT"`
	RedisRateLimitTTL    string `env:"REDIS_RATE_LIMIT_TTL"`
	RedisRateLimitMax    int    `env:"REDIS_RATE_LIMIT_MAX"`
	FileStoragePath      string `env:"FILE_STORAGE_PATH"`
	BaseFileURL          string `env:"BASE_FILE_URL"`
	ClamAVAddress        string `env:"CLAMAV_ADDRESS"`
	ClamAVTimeout        string `env:"CLAMAV_TIMEOUT"`
	AWSRegion            string `env:"AWS_REGION"`
	AWSAccessKeyID       string `env:"AWS_ACCESS_KEY_ID,secret"`
	AWSSecretAccessKey   string `env:"AWS_SECRET_ACCESS_KEY,secret"`
	JWTRSAPrivateKey     string `env:"JWT_RSA_PRIVATE_KEY,secret"`
	JWTRSAPublicKey      string `env:"JWT_RSA_PUBLIC_KEY,secret"`
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Environment:          getEnv("ENVIRONMENT", "development"),
		Port:                 getEnv("PORT", "8080"),
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		ClamAVAddress:        getEnv("CLAMAV_ADDRESS", ""),
		ClamAVTimeout:        getEnv("CLAMAV_TIMEOUT", "5s"),
		AWSRegion:            getEnv("AWS_REGION", ""),
		AWSAccessKeyID:       getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:   getEnv("AWS_SECRET_ACCESS_KEY", ""),
		JWTRSAPrivateKey:     getEnv("JWT_RSA_PRIVATE_KEY", ""),
		JWTRSAPublicKey:      getEnv("JWT_RSA_PUBLIC_KEY", ""),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379/0"),
		RedisPassword:        getEnv("REDIS_PASSWORD", ""),
		RedisDB:              getEnvAsInt("REDIS_DB", 0),
		RedisPoolMaxIdle:     getEnvAsInt("REDIS_POOL_MAX_IDLE", 80),
		RedisPoolMaxActive:   getEnvAsInt("REDIS_POOL_MAX_ACTIVE", 12000),
		RedisPoolIdleTimeout: getEnv("REDIS_POOL_IDLE_TIMEOUT", "300s"),
		RedisRateLimitTTL:    getEnv("REDIS_RATE_LIMIT_TTL", "60s"),
		RedisRateLimitMax:    getEnvAsInt("REDIS_RATE_LIMIT_MAX", 100),
		FileStoragePath:      getEnv("FILE_STORAGE_PATH", "./uploads"),
		BaseFileURL:          getEnv("BASE_FILE_URL", "/files"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		intValue, err := strconv.Atoi(value)
		if err == nil {
			return intValue
		}
	}
	return defaultValue
}
