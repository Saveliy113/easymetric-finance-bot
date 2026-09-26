package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"em-finance-bot/internal/domain"
	"em-finance-bot/internal/service/ai"
	"em-finance-bot/internal/service/sheets"

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

	return c.Send(categoriesPromptText, markup, telebot.ModeMarkdown)
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
		slog.Int64("user_id", user.TelegramID),
		slog.String("categories", inputCategories),
	)

	if len(inputCategories) < 2 {
		return domain.ErrInvalidCategories
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Анализирую категории трат...")

	slog.InfoContext(ctx, "Отправляем запрос в gemini для анализа категорий",
		slog.Int64("user_id", user.TelegramID),
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
		slog.Int64("user_id", user.TelegramID),
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

func (r *Router) sendCategorySuggestions(ctx context.Context, c telebot.Context, transaction *ai.ParsedTransaction, user *domain.User) error {
	// Unmarshaling user categories
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return fmt.Errorf("failed to unmarshal categories: %w", err)
	}

	// Defining remaining categories
	suggestedSet := make(map[string]bool)
	for _, cat := range transaction.SuggestedCategories {
		clean := strings.ToLower(strings.TrimSpace(cat))
		suggestedSet[clean] = true
	}

	var remainingCategories []string
	for _, cat := range categories {
		clean := strings.ToLower(strings.TrimSpace(cat))
		if !suggestedSet[clean] {
			remainingCategories = append(remainingCategories, cat)
		}
	}

	// Format clarification message using ModeHTML
	messageText := fmt.Sprintf(
		"💳 <b>Транзакция</b>\n"+
			"• <b>Категория:</b> <i>Не определена</i>\n"+
			"• <b>Сумма:</b> <code>%.2f %s</code>\n"+
			"• <b>Описание:</b> %s\n"+
			"• <b>Дата:</b> <code>%s</code>\n\n"+
			"🤔 <b>Не удалось точно определить категорию для этой операции.</b>\n"+
			"Советую отнести к одной из предложенных категорий ниже 👇",
		transaction.Amount,
		user.Currency,
		transaction.Description,
		transaction.Date.Format("02.01.2006 15:04"),
	)

	// Building inline keyboard with suggested categories
	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, cat := range transaction.SuggestedCategories {
		btn := menu.Data("💡 "+cat, btnIDSelectClarifiedCategory, cat)
		rows = append(rows, menu.Row(btn))
	}

	// Remaining buttons from user settings (grid style - 2 buttons per row)
	if len(remainingCategories) > 0 {
		var currentRow []telebot.Btn
		for _, cat := range remainingCategories {
			btn := menu.Data(cat, btnIDSelectClarifiedCategory, cat)
			currentRow = append(currentRow, btn)

			if len(currentRow) == 2 {
				rows = append(rows, menu.Row(currentRow...))
				currentRow = nil
			}
		}

		// Pushing remaining button one in a row
		if len(currentRow) > 0 {
			rows = append(rows, menu.Row(currentRow...))
		}
	}

	// Cancelation button
	btnCancel := menu.Data("❌ Отменить запись", btnIDCancelTransactionClarification)
	rows = append(rows, menu.Row(btnCancel))

	menu.Inline(rows...)

	// Saving pending transaction in the db
	transactionBytes, err := json.Marshal(transaction)
	if err != nil {
		return fmt.Errorf("marshal pending tx: %w", err)
	}

	user.PendingTransaction = string(transactionBytes)
	user.State = domain.StateAwaitingCategoryClarification

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Sending clarification message
	return c.Send(messageText, menu, telebot.ModeHTML)
}

func (r *Router) sendManualCategorySelection(ctx context.Context, c telebot.Context, transaction *ai.ParsedTransaction, user *domain.User) error {
	// Unmarshaling user categories from the db
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return fmt.Errorf("failed to unmarshal categories: %w", err)
	}

	// Format clarification message using ModeHTML
	messageText := fmt.Sprintf(
		"💳 <b>Транзакция</b>\n"+
			"• <b>Категория:</b> <i>Не определена</i>\n"+
			"• <b>Сумма:</b> <code>%.2f %s</code>\n"+
			"• <b>Описание:</b> %s\n"+
			"• <b>Дата:</b> <code>%s</code>\n\n"+
			"🤔 <b>Не удалось точно определить категорию для этой операции.</b>\n"+
			"Советую отнести к одной из предложенных категорий ниже 👇",
		transaction.Amount,
		user.Currency,
		transaction.Description,
		transaction.Date.Format("02.01.2006 15:04"),
	)

	// Building inline keyboard from user configured categories
	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	var currentRow []telebot.Btn
	for _, cat := range categories {
		btn := menu.Data(cat, btnIDSelectClarifiedCategory, cat)
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 {
			rows = append(rows, menu.Row(currentRow...))
			currentRow = nil
		}
	}

	// Pushing remaining button one in a row
	if len(currentRow) > 0 {
		rows = append(rows, menu.Row(currentRow...))
	}

	// Cancelation button
	btnCancel := menu.Data("❌ Отменить запись", btnIDCancelTransactionClarification)
	rows = append(rows, menu.Row(btnCancel))

	menu.Inline(rows...)

	return c.Send(messageText, menu, telebot.ModeHTML)
}

func (r *Router) handleSelectClarifiedCategory(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to user
	_ = c.Respond()

	// Delete inline clarification buttons
	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

	// Checking that user still in AwaitingCategoryClarification state
	slog.InfoContext(
		ctx, "Проверка состяния пользователя (ожидание уточнения категории транзакции)",
		slog.Int64("user_id", c.Sender().ID),
	)

	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	if user.State != domain.StateAwaitingCategoryClarification {
		slog.InfoContext(
			ctx,
			"Невозможно сохранить транзакцию: истек срок уточнения",
			slog.Int64("user_id", user.TelegramID),
			slog.String("current_state", string(user.State)),
		)
		return domain.ErrTransactionClarificationExpired
	}

	// Getting category from button text
	category := c.Data()
	slog.InfoContext(ctx, "Выбрана категория: ", slog.String("category", category))

	// Unmarshaling pending transaction
	var transaction ai.ParsedTransaction
	if err := json.Unmarshal([]byte(user.PendingTransaction), &transaction); err != nil {
		return fmt.Errorf("failed to unmarshal pending transaction: %w", err)
	}

	slog.InfoContext(
		ctx,
		"Ожидаемая транзакция",
		slog.Any("transaction", transaction),
	)

	// Assigning category to transaction
	transaction.Category = category

	// Saving transaction to google sheets
	nextTxID, err := r.userRepo.IncrementLastTransactionID(ctx, user.TelegramID)
	if err != nil {
		return fmt.Errorf("error incrementing transaction id: %w", err)
	}
	user.LastTransactionID = nextTxID

	slog.InfoContext(ctx, "Сохраняем операцию в Google Таблицу",
		slog.Int64("user_id", user.TelegramID),
		slog.String("spreadsheet_id", user.SpreadsheetID),
		slog.Int("transaction_id", nextTxID),
	)

	if err = r.sheetsService.SaveTransaction(ctx, user.SpreadsheetID, &sheets.Transaction{
		ID:          int64(nextTxID),
		UserID:      user.TelegramID,
		Type:        sheets.TransactionType(transaction.Type),
		Amount:      transaction.Amount,
		Category:    transaction.Category,
		Description: transaction.Description,
		Date:        transaction.Date,
		CreatedAt:   time.Now(),
	}); err != nil {
		return err
	}

	// Cleaning pending transaction
	slog.InfoContext(
		ctx,
		"Очистка pending-транзакции и установка состояния Ready",
		slog.Int64("user_id", user.TelegramID),
	)

	user.PendingTransaction = ""
	user.State = domain.StateReady

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Send result message
	// TODO: Refactor maybe - create a separate function in markup
	var textResponse string
	if transaction.Type == string(sheets.TypeIncome) {
		textResponse = fmt.Sprintf("✅ <b>Доход записан!</b>\n\n🆔 ID: <b>#%d</b>\n💰 Сумма: <b>%.2f</b>\n📝 Описание: %s\n📅 Дата: %s",
			nextTxID,
			transaction.Amount,
			transaction.Description,
			transaction.Date.Format("02.01.2006 15:04"),
		)
	} else {
		textResponse = fmt.Sprintf("✅ <b>Расход записан!</b>\n\n🆔 ID: <b>#%d</b>\n💸 Сумма: <b>%.2f</b>\n📁 Категория: <b>%s</b>\n📝 Описание: %s\n📅 Дата: %s",
			nextTxID,
			transaction.Amount,
			transaction.Category,
			transaction.Description,
			transaction.Date.Format("02.01.2006 15:04"),
		)
	}

	return c.Send(textResponse, telebot.ModeHTML)
}

func (r *Router) handleCancelTransactionClarification(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to user
	_ = c.Respond()

	// Getting user
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err == nil && user != nil {
		user.State = domain.StateReady
		user.PendingTransaction = ""
		_ = r.userRepo.Upsert(ctx, user)
	}

	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)
	return c.Send("❌ Запись транзакции отменена.")
}

// TODO: move "Категории успешно подключены" to the previous function and move the remaining to sheets
func (r *Router) handleSheetStep(ctx context.Context, c telebot.Context) error {
	slog.InfoContext(ctx, "Отправка шага подключения Google Таблицы",
		slog.Int64("user_id", c.Sender().ID),
	)

	nextStepText := fmt.Sprintf(
		"✅ <b>Категории успешно подключены!</b>\n\n"+
			"📍 <b>Шаг 3 из 3: Подключение Google Таблицы</b>\n\n"+
			"1. <a href=\"%s\">Создай копию шаблона таблицы EM Personal Finances</a>\n"+
			"2. Выдай доступ на редактирование сервисному аккаунту бота:\n<code>%s</code>\n\n"+
			"3. Отправь ссылку на свою готовую копию таблицы в ответном сообщении:",
		r.cfg.TemplateSheetURL,
		r.cfg.GoogleServiceAccountEmail,
	)

	photo := &telebot.Photo{
		File:    telebot.FromDisk("assets/images/em_fin_template_access.png"),
		Caption: nextStepText,
	}

	return c.Send(photo, telebot.ModeHTML)
}
