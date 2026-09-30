package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port string
	BotToken string
	PublicURL string
	TelegramSecretToken string
	TemplateSheetURL string
	GoogleServiceAccountEmail string
	GeminiAPIKey string
	GoogleCredentialsPath string
}

func LoadConfig() *Config {
	env := os.Getenv("APP_ENV")
	envFile := ".env.dev" // Default to development environment

	// If APP_ENV is set, use the corresponding .env file
	if env != "" {
		envFile = ".env." + env
	}

	// Try loading environment variables from envFile, or fallback to .env if it exists.
	// In production or container environments, variables can be passed directly via system environment.
	if err := godotenv.Load(envFile); err != nil {
		if errDefault := godotenv.Load(".env"); errDefault != nil {
			log.Printf("Notice: No env file loaded (%s or .env), using system environment variables", envFile)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		log.Printf("Port is not set, setting default port :7070")
		port = "7070" // Default port if not set
	}

	return &Config{
		Port: port,
		BotToken: os.Getenv("BOT_TOKEN"),
		PublicURL: os.Getenv("PUBLIC_URL"),
		TelegramSecretToken: os.Getenv("TELEGRAM_SECRET_TOKEN"),
		TemplateSheetURL: os.Getenv("TEMPLATE_SHEET_URL"),
		GoogleServiceAccountEmail: os.Getenv("GOOGLE_SERVICE_ACCOUNT_EMAIL"),
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		GoogleCredentialsPath: os.Getenv("GOOGLE_CREDENTIALS_PATH"),
	}
}