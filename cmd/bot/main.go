package main

import (
	"em-finance-bot/config"
	"em-finance-bot/internal/app"
)

func main() {
	cfg := config.LoadConfig()
	app.Run(cfg)
}