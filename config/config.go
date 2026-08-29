package config

import "os"

type Config struct {
	Port string
}

func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7070" // Default port if not set
	}

	return &Config{
		Port: port,
	}
}