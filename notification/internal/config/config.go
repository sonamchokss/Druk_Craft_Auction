package config

import "os"

type Config struct {
	RabbitURL   string
	DatabaseURL string
	Port        string
}

func Load() Config {
	return Config{
		RabbitURL:   env("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/notifications_db?sslmode=disable"),
		Port:        env("PORT", "8005"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
