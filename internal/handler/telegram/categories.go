package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"em-finance-bot/internal/domain"

	"gopkg.in/telebot.v3"
)

var defaultCategories = []string{
	"Продукты",
	"Кафе и рестораны",
	"Транспорт",
	"Покупки",
	"Развлечения",
	"Здоровье",
	"Регулярные платежи",
	"Прочее",
}

func (r *Router) handleCategoriesStep(ctx context.Context, c telebot.Context) error {
	slog.InfoContext(ctx, "Отправка шага настройки категорий",
		slog.Int64("user_id", c.Sender().ID),
	)

	markup := r.defaultCategoriesMarkup()
	categoriesPromptText := "📍 *Шаг 2 из 3: Настройка категорий трат*\n\n" +
		"Категории помогают боту автоматически распределять твои расходы.\n\n" +
		"Вот готовый сбалансированный набор:\n" +
		"• 🛒 *Продукты* — супермаркеты, бакалея, еда\n" +
		"• ☕ *Кафе и рестораны* — кофе, фастфуд, бары\n" +
		"• 🚗 *Транспорт* — такси, бензин, проездной\n" +
		"• 🛍 *Покупки* — одежда, техника, дом\n" +
		"• 🎉 *Развлечения* — кино, спорт, отдых\n" +
		"• 💊 *Здоровье* — аптеки, врачи\n" +
		"• 🔄 *Регулярные платежи* — аренда, связь, подписки\n" +
		"• 📦 *Прочее* — подарки, непредвиденные траты\n\n" +
		"---\n" +
		"Выбери действие:\n" +
		"• Нажми кнопку ниже, чтобы применить этот набор\n" +
		"• Либо отправь свой список через запятую (например: _Еда, Авто, Дом, Хобби_)"

	return c.Send(categoriesPromptText,
		markup,
		telebot.ModeMarkdown)
}

func (r *Router) handleUseDefaultCategories(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)
	sender := c.Sender()

	slog.InfoContext(ctx, "Выбраны стандартные категории",
		slog.Int64("user_id", sender.ID),
	)

	_ = c.Respond()
	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

	// Serializing default categories for saving in the db
	categoriesBytes, err := json.Marshal(defaultCategories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, sender.ID)
	if err != nil {
		return err
	}

	// Setup user categories in sheets if spreadsheet is already connected
	if user.SpreadsheetID != "" {
		if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", defaultCategories); err != nil {
			return err
		}
	}

	// Saving user to the db
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	return r.handleSheetStep(ctx, c)
}

func (r *Router) handleUserCustomCategories(ctx context.Context, c telebot.Context, user *domain.User) error {
	inputCategories := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Пользовательские категории:",
		slog.String("categories", inputCategories),
	)

	if len(inputCategories) < 2 {
		return domain.ErrInvalidCategories
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Анализирую категории трат...")

	slog.InfoContext(ctx, "Отправляем запрос в gemini для анализа категорий",
		slog.String("categories", inputCategories),
	)

	categoriesInfo, err := r.aiService.ParseCategories(ctx, inputCategories)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil || !categoriesInfo.IsValid {
		return domain.ErrParsingCategories
	}

	slog.InfoContext(ctx, "Получены категории от gemini",
		slog.Any("categories", categoriesInfo.Categories),
	)

	categoriesBytes, err := json.Marshal(categoriesInfo.Categories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Setup user categories in sheets if spreadsheet is already connected
	if user.SpreadsheetID != "" {
		if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", categoriesInfo.Categories); err != nil {
			return err
		}
	}

	slog.InfoContext(ctx, "Обновляем кеш категорий",
		slog.Int64("user_id", user.TelegramID),
	)
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Переводим пользователя в ожидание ссылки на таблицу",
		slog.Int64("user_id", user.TelegramID),
	)

	return r.handleSheetStep(ctx, c)
}

func (r *Router) handleSheetStep(ctx context.Context, c telebot.Context) error {
	slog.InfoContext(ctx, "Отправка шага подключения Google Таблицы",
		slog.Int64("user_id", c.Sender().ID),
	)

	nextStepText := fmt.Sprintf(
		"✅ Категории успешно подключены!\n\n"+
			"📍 *Шаг 3 из 3: Подключение Google Таблицы*\n\n"+
			"1. [Создай копию шаблона таблицы EM Personal Finances](%s)\n"+
			"2. Выдай доступ на редактирование сервисному аккаунту бота:\n`%s`\n\n"+
			"3. Отправь ссылку на свою готовую копию таблицы в ответном сообщении:",
		r.cfg.TemplateSheetURL,
		r.cfg.GoogleServiceAccountEmail,
	)

	return c.Send(nextStepText, telebot.ModeMarkdown)
}
