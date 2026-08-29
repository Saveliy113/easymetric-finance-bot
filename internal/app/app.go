package app

import (
	"fmt"
	"log"

	"em-finance-bot/config"

	"github.com/gofiber/fiber/v3"
)

func Run(cfg *config.Config) {
	app := fiber.New(fiber.Config{
		AppName: "Easymetric Finance Bot",
	})

	app.Get("/alive", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": "alive",
		})
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Server started on port %s", cfg.Port)

	if err := app.Listen(addr); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
