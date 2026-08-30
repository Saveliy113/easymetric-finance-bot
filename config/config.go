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
}

func LoadConfig() *Config {
	env := os.Getenv("APP_ENV")
	envFile := ".env.dev" // Default to development environment

	// If APP_ENV is set, use the corresponding .env file
	if env != "" {
		envFile = ".env." + env
	}

	// Checking if the .env file exists
	if _, err := os.Stat(envFile); err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Environment file %s does not exist", envFile)
		}

		log.Fatalf("Failed to check environment file %s: %v", envFile, err)
	}

	// Load environment variables from .env file if it exists
	if err := godotenv.Load(envFile); err != nil {
		log.Fatalf("Error loading %s file: %v", envFile, err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		log.Println("Port is not set in .env file, setting default port :7070")
		port = "7070" // Default port if not set
	}

	return &Config{
		Port: port,
		BotToken: os.Getenv("BOT_TOKEN"),
		PublicURL: os.Getenv("PUBLIC_URL"),
		TelegramSecretToken: os.Getenv("TELEGRAM_SECRET_TOKEN"),
		TemplateSheetURL: os.Getenv("TEMPLATE_SHEET_URL"),
		GoogleServiceAccountEmail: os.Getenv("GOOGLE_SERVICE_ACCOUNT_EMAIL"),
	}
}