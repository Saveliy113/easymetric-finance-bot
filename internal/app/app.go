package app

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"em-finance-bot/config"
	httpHandler "em-finance-bot/internal/handler/http"
	"em-finance-bot/internal/handler/telegram"
	tgHandler "em-finance-bot/internal/handler/telegram"
	db "em-finance-bot/internal/repository/sqlite"
	ai "em-finance-bot/internal/service/ai"
	sheets "em-finance-bot/internal/service/sheets"
	"em-finance-bot/pkg/logger"

	"github.com/gofiber/fiber/v3"
	"gopkg.in/telebot.v3"
)

func Run(cfg *config.Config) {
	ctx := context.Background()

	// Configuring global logger
	baseLogHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	traceHandler := logger.NewTraceHandler(baseLogHandler)
	slog.SetDefault(slog.New(traceHandler))

	// Initializing the database
	db.Init()

	// Initializing Gemini service
	geminiService := ai.NewGeminiService(ctx, cfg.GeminiAPIKey)

	// Initializing Google Sheets service
	sheetsService := sheets.NewSheetService(ctx, cfg.GoogleCredentialsPath)

	// Initializing repositories
	userRepo := db.NewUserRepository(db.DB)

	// Initializing Telegram Bot
	bot, err := telebot.NewBot(telebot.Settings{
		Token:   cfg.BotToken,
		Offline: true,
		OnError: telegram.CatchUnhandledErrors,
	})
	if err != nil {
		log.Fatalf("Error initializing Telegram bot: %v", err)
	}

	// Registering Telegram commands and events
	tgRouter := tgHandler.NewRouter(bot, cfg, userRepo, geminiService, sheetsService)
	tgRouter.Register()

	webHookUrl := fmt.Sprintf("%s/webhook/telegram", cfg.PublicURL)
	err = bot.SetWebhook(&telebot.Webhook{
		Endpoint:    &telebot.WebhookEndpoint{PublicURL: webHookUrl},
		SecretToken: cfg.TelegramSecretToken,
	})

	if err != nil {
		log.Fatalf("Error installing telegram webhook: %v", err)
	}
	log.Printf("🔗 Telegram webhook installed: %s", webHookUrl)

	// Initializing Fiber web server
	app := fiber.New(fiber.Config{
		AppName: "Easymetric Finance Bot",
	})

	app.Get("/alive", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": "alive",
		})
	})

	// Registering HTTP routes for Telegram webhook
	webhookHandler := httpHandler.NewWebhookHandler(bot, cfg.TelegramSecretToken)
	webhookHandler.Register(app)

	// Starting the server
	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Server started on port %s", cfg.Port)

	if err := app.Listen(addr); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
