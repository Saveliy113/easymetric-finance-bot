package http

import (
	"log"

	"github.com/gofiber/fiber/v3"
	"gopkg.in/telebot.v3"
)

type WebhookHandler struct {
	bot *telebot.Bot
	secretToken string
}

func NewWebhookHandler(bot *telebot.Bot, secretToken string) *WebhookHandler {
	return &WebhookHandler{
		bot: bot,
		secretToken: secretToken,
	}
}

// Register routes in fiber app
func (h *WebhookHandler) Register(app *fiber.App) {
	// Telegram webhook endpoint
	app.Post("/webhook/telegram", h.HandleTelegramUpdate)
}

func (h *WebhookHandler) HandleTelegramUpdate(c fiber.Ctx) error {
	// Validating secret token from telegram (spam protection)
	if h.secretToken != "" {
		incomingToken := c.Get("X-Telegram-Bot-Api-Secret-Token")
		if incomingToken != h.secretToken {
			log.Printf("⚠️ Incoming request with invalid secret token was rejected: %s", incomingToken)
			return c.SendStatus(fiber.StatusUnauthorized)
		}
	}

	// JSON parsing request body into telebot.Update struct
	var update telebot.Update
	if err := c.Bind().Body(&update); err != nil {
		log.Printf("❌ Error parsing JSON update: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "failed to parse update payload",
		})
	}

	// Asynchronously process the update
	go h.bot.ProcessUpdate(update)

	// Returning 200 OK to Telegram to acknowledge receipt of the update
	return c.SendStatus(fiber.StatusOK)
}
