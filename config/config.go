package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	UniversityURL  string
	Username       string
	Password       string
	TelegramToken  string
	TelegramChatID string
	PollInterval   time.Duration
	StateFile      string
	UserAgent      string
}

func Load() *Config {
	intervalSec, err := strconv.Atoi(getEnv("POLL_INTERVAL_SECONDS", "60"))
	if err != nil {
		log.Fatal("POLL_INTERVAL_SECONDS geçersiz")
	}
	return &Config{
		UniversityURL:  mustGetEnv("UNIVERSITY_URL"),
		Username:       mustGetEnv("UNIVERSITY_USER"),
		Password:       mustGetEnv("UNIVERSITY_PASS"),
		TelegramToken:  mustGetEnv("TELEGRAM_TOKEN"),
		TelegramChatID: mustGetEnv("TELEGRAM_CHAT_ID"),
		PollInterval:   time.Duration(intervalSec) * time.Second,
		StateFile:      getEnv("STATE_FILE", "/data/state.json"),
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("zorunlu env eksik: %s", key)
	}
	return strings.TrimSpace(v)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
