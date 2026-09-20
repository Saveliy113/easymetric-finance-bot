package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleCategoriesStep(c telebot.Context) error {
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
	fmt.Println("USER CHOOSED DEFAULT CATEGORIES")
	ctx := context.Background()
	senderId := c.Sender().ID

	defaultCategoriesList := [8]string{
		"Продукты",
		"Кафе и рестораны",
		"Транспорт",
		"Покупки",
		"Развлечения",
		"Здоровье",
		"Регулярные платежи",
		"Прочее",
	}

	// Serializing default categories for saving in the db
	categoriesBytes, err := json.Marshal(defaultCategoriesList)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении профиля. Попробуй позже.")
	}
	if user == nil {
		return c.Send("⚠️ Профиль не найден. Начни с команды /start.")
	}

	// Setup user categories in sheets
	if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", defaultCategoriesList[:]); err != nil {
		slog.Error("Не удалось настроить категории", "ошибка", err)
	}

	// Saving user to the db
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Не удалось сохранить категории. Попробуй еще раз.")
	}

	// 6. Отправляем инструкцию к Шагу 3 (Google Sheets)
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

func (r *Router) handleUserCustomCategories(c telebot.Context) error {
	ctx := context.Background()
	senderId := c.Sender().ID
	inputCategories := strings.TrimSpace(c.Text())

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Анализирую категории трат...")

	// Getting data using gemini
	categoriesInfo, err := r.aiService.ParseCategories(ctx, inputCategories)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		fmt.Printf("Error while detecting categories: %v", err)
		return c.Send("⚠️ Ошибка при анализе категорий. Попробуй еще раз.")

	}

	if !categoriesInfo.IsValid && categoriesInfo.ErrorMessage != "" {
		return c.Send(categoriesInfo.ErrorMessage)
	}

	fmt.Println("USER CHOOSED CUSTOM CATEGORIES:", categoriesInfo.Categories)

	// Saving user's categories to db
	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении профиля. Попробуй позже.")
	}
	if user == nil {
		return c.Send("⚠️ Профиль не найден. Начни с команды /start.")
	}

	// Serializing default categories for saving in the db
	categoriesBytes, err := json.Marshal(categoriesInfo.Categories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Setup user categories in sheets
	if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", categoriesInfo.Categories); err != nil {
		slog.Error("Не удалось настроить категории", "ошибка", err)
	}

	// Saving user to the db
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Не удалось сохранить категории. Попробуй еще раз.")
	}

	// Sending instructions for step 3 - connecting Google Sheets
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
