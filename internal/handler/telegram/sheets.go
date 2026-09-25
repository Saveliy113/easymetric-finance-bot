package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"em-finance-bot/internal/domain"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleSheetURLInput(ctx context.Context, c telebot.Context, user *domain.User) error {
	sheetUrl := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Получена ссылка на Google Таблицу:",
		slog.Int64("user_id", user.TelegramID),
		slog.String("url", sheetUrl),
	)

	// Extracting unique sheet id from url
	slog.InfoContext(ctx, "Извлекаем уникальный id таблицы из ссылки")
	sheetIDRegex := regexp.MustCompile(`/d/([a-zA-Z0-9_-]+)`)
	matches := sheetIDRegex.FindStringSubmatch(sheetUrl)
	if len(matches) < 2 {
		return domain.ErrInvalidSheetURL
	}

	sheetID := matches[1]

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Проверяю доступ к таблице...")

	// Checking the bot is able to operate with the table
	slog.InfoContext(ctx, "Проверяем доступ к Google Таблице",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	err := r.sheetsService.ValidateAccess(ctx, sheetID)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Доступ к Google Таблице успешно подтвержден",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	// Setup user categories in the connected sheet if available
	if user.CategoriesCache != "" {
		var categories []string
		if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
			return fmt.Errorf("failed to unmarshal categories: %w", err)
		}
		if len(categories) > 0 {
			if err := r.sheetsService.SetupUserCategories(ctx, sheetID, "Дашборд", categories); err != nil {
				return err
			}
		}
	}

	// Saving sheet id to the db
	slog.InfoContext(ctx, "Сохраняем id таблицы в БД",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	user.SpreadsheetID = sheetID
	user.State = domain.StateReady

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Id таблицы успешно сохранен в БД",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	// Sending welcome message
	welcomeMessage := fmt.Sprintf(
		"✅ *Отлично! Твоя персональная финансовая система готова к работе.*\n\n"+
			"🔗 *Таблица:* [Открыть в Google Sheets](%s)\n\n"+
			"Теперь ты можешь отправлять мне свои финансовые операции в свободной форме:\n\n"+
			"Примеры:\n"+
			"• *«Купил кофе за 300 рублей»*\n"+
			"• *«Обед 850»*\n"+
			"• *«Пополнил баланс на 1000»*\n\n"+
			"Каждую операцию я буду автоматически записывать в твою таблицу, классифицировать по категориям и обновлять все необходимые расчеты.\n\n"+
			"Я постараюсь максимально точно определить категории для твоих трат, но при необходимости всегда смогу задать уточняющие вопросы.\n\n"+
			"Для быстрого доступа ко всем функциям используй меню внизу.",
		sheetUrl,
	)

	return c.Send(welcomeMessage, telebot.ModeMarkdown)
}
