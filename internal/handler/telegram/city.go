package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"fmt"
	"strings"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleCityInput(c telebot.Context, user *domain.User) error {
	ctx := context.Background()
	inputCity := strings.TrimSpace(c.Text())

	// Empty strings or too short city names guard
	if len(inputCity) < 2 {
		return c.Send("Пожалуйста, напиши корректное название города:")
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Определяю часовой пояс и валюту...")

	// Getting data using gemini
	locationInfo, err := r.aiService.ParseCity(ctx, inputCity)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil || !locationInfo.IsValid {
		return c.Send(
			"Не удалось распознать город 😔\nПопробуй написать название ещё раз (например: *Алматы*, *Москва*, *Тбилиси*):",
			telebot.ModeMarkdown,
		)
	}

	// Updating user location data and state
	user.Timezone = locationInfo.Timezone
	user.Currency = locationInfo.Currency
	user.State = domain.StateAwaitingCategories

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Ошибка при сохранении данных в базу. Попробуй ещё раз.")
	}

	// 1. Первое сообщение: подтверждение распознанных данных
	locationSummary := fmt.Sprintf(
		"✅ Город определен: *%s*\n"+
			"🕒 Часовой пояс: `%s`\n"+
			"💱 Валюта по умолчанию: `%s`",
		locationInfo.City,
		locationInfo.Timezone,
		locationInfo.Currency,
	)

	if err := c.Send(locationSummary, telebot.ModeMarkdown); err != nil {
		return err
	}

	err = r.handleCategoriesStep(c)
	if err != nil {
		return err
	}

	return nil
}
