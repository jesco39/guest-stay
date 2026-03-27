package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port                 string
	GuestPassword        string
	AdminUsername         string
	AdminPassword         string
	GoogleCredentialsFile string
	GoogleCalendarID     string
	SMTPHost             string
	SMTPPort             string
	SMTPUsername          string
	SMTPPassword         string
	SMTPFrom             string
	AdminEmails          []string
}

func loadConfig() (*Config, error) {
	loadEnvFile(".env")

	cfg := &Config{
		Port:                 getEnv("PORT", "8080"),
		GuestPassword:        getEnv("GUEST_PASSWORD", ""),
		AdminUsername:         getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:         getEnv("ADMIN_PASSWORD", ""),
		GoogleCredentialsFile: getEnv("GOOGLE_CREDENTIALS_FILE", "./credentials.json"),
		GoogleCalendarID:     getEnv("GOOGLE_CALENDAR_ID", ""),
		SMTPHost:             getEnv("SMTP_HOST", ""),
		SMTPPort:             getEnv("SMTP_PORT", "587"),
		SMTPUsername:         getEnv("SMTP_USERNAME", ""),
		SMTPPassword:         getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:             getEnv("SMTP_FROM", ""),
	}

	if emails := getEnv("ADMIN_EMAILS", ""); emails != "" {
		for _, e := range strings.Split(emails, ",") {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				cfg.AdminEmails = append(cfg.AdminEmails, trimmed)
			}
		}
	}

	if cfg.GuestPassword == "" {
		return nil, fmt.Errorf("GUEST_PASSWORD is required")
	}
	if cfg.AdminPassword == "" {
		return nil, fmt.Errorf("ADMIN_PASSWORD is required")
	}

	return cfg, nil
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
