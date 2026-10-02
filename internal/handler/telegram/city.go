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
	slog.InfoContext(ctx, "Город пользователя:",
		slog.Int64("user_id", user.TelegramID),
		slog.String("city", inputCity),
	)

	// Empty strings or too short city names guard
	if len(inputCity) < 2 {
		return domain.ErrInvalidCity
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Определяю часовой пояс и валюту...")
	_ = c.Notify(telebot.Typing)

	// Getting data using gemini
	slog.InfoContext(ctx, "Отправляем запрос в gemini для определения часового пояса и валюты",
		slog.Int64("user_id", user.TelegramID),
		slog.String("city", inputCity),
	)

	locationInfo, err := r.aiService.ParseCity(ctx, inputCity)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	// Separate infrastructure errors from validation failures
	if err != nil {
		// Gemini is down, network error, etc. — NOT the user's fault
		return err
	}
	if !locationInfo.IsValid {
		// Gemini responded correctly but said the city is invalid
		return domain.ErrParsingCity
	}

	slog.InfoContext(ctx, "Получены данные от gemini",
		slog.Int64("user_id", user.TelegramID),
		slog.String("timezone", locationInfo.Timezone),
		slog.String("currency", locationInfo.Currency),
	)

	// Updating user location data and state
	user.Timezone = locationInfo.Timezone
	user.Currency = locationInfo.Currency

	// If updating location, returning state to reade
	// Otherwise, continue onboarding flow
	isSettingsFlow := user.SpreadsheetID != ""
	if isSettingsFlow {
		user.State = domain.StateReady
	} else {
		user.State = domain.StateAwaitingCategories
	}

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Different headers for different flows
	header := "✅ Город определен:"
	if isSettingsFlow {
		header = "✅ *Город и часовой пояс успешно обновлены!*\n\n📍 Город:"
	}

	locationSummary := fmt.Sprintf(
		"%s *%s*\n"+
			"🕒 Часовой пояс: `%s`\n"+
			"💱 Валюта по умолчанию: `%s`",
		header,
		locationInfo.City,
		locationInfo.Timezone,
		locationInfo.Currency,
	)

	if err := c.Send(locationSummary, telebot.ModeMarkdown); err != nil {
		return err
	}

	// Skip categorie step for updating location
	// And continue onboarding flow otherwise
	if isSettingsFlow {
		return nil
	}

	return r.handleCategoriesStep(ctx, c)
}
