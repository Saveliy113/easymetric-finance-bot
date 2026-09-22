package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"fmt"
	"strings"

	"gopkg.in/telebot.v3"
	"log/slog"
)

func (r *Router) handleCityInput(ctx context.Context, c telebot.Context, user *domain.User) error {
	inputCity := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Город пользователя:", slog.String("city", inputCity))

	// Empty strings or too short city names guard
	if len(inputCity) < 2 {
		return domain.ErrInvalidCity
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Определяю часовой пояс и валюту...")

	// Getting data using gemini
	slog.InfoContext(ctx, "Отправляем запрос в gemini для определения часового пояса и валюты", slog.String("city", inputCity))
	locationInfo, err := r.aiService.ParseCity(ctx, inputCity)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil || !locationInfo.IsValid {
		return domain.ErrParsingCity
	}

	slog.InfoContext(ctx, "Получены данные от gemini", slog.String("timezone", locationInfo.Timezone), slog.String("currency", locationInfo.Currency))

	// Updating user location data and state
	user.Timezone = locationInfo.Timezone
	user.Currency = locationInfo.Currency
	user.State = domain.StateAwaitingCategories

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
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

	if err := r.handleCategoriesStep(ctx, c); err != nil {
		return err
	}

	return nil
}
