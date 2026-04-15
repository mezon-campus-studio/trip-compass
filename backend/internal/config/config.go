package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost         string
	DBPort         string
	DBUser         string
	DBPassword     string
	DBName         string
	DBSchema       string
	RedisAddr      string
	RedisPassword  string
	JWTSecret      string
	Port           string
	AllowedOrigins string
	// LLM planner proxy
	UseLLMPlanner  bool   // USE_LLM_PLANNER=true → proxy to PlannerAIURL
	PlannerAIURL   string // PLANNER_AI_URL=http://planner-ai:8090
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env not found: %v", err)
	}

	cfg := &Config{
		DBHost:         os.Getenv("DB_HOST"),
		DBPort:         os.Getenv("DB_PORT"),
		DBUser:         os.Getenv("DB_USER"),
		DBPassword:     os.Getenv("DB_PASSWORD"),
		DBName:         os.Getenv("DB_NAME"),
		DBSchema:       os.Getenv("DB_SCHEMA"),
		RedisAddr:      os.Getenv("REDIS_ADDR"),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		Port:           os.Getenv("PORT"),
		AllowedOrigins: os.Getenv("ALLOWED_ORIGINS"),
		UseLLMPlanner:  os.Getenv("USE_LLM_PLANNER") == "true",
		PlannerAIURL:   os.Getenv("PLANNER_AI_URL"),
	}

	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	if cfg.AllowedOrigins == "" {
		cfg.AllowedOrigins = "http://localhost:3000"
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	return cfg
}
