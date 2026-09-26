package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"em-finance-bot/internal/domain"
	"em-finance-bot/internal/service/ai"

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
	var aiTransactionData ai.ParsedTransaction
	if err := json.Unmarshal([]byte(user.PendingTransaction), &aiTransactionData); err != nil {
		return fmt.Errorf("failed to unmarshal pending transaction: %w", err)
	}

	slog.InfoContext(
		ctx,
		"Ожидаемая транзакция",
		slog.Any("transaction", aiTransactionData),
	)

	// Assigning category to transaction
	aiTransactionData.Category = category

	// Saving transaction to google sheets
	nextTransactionID, err := r.userRepo.IncrementLastTransactionID(ctx, user.TelegramID)
	if err != nil {
		return fmt.Errorf("error incrementing transaction id: %w", err)
	}
	user.LastTransactionID = nextTransactionID

	slog.InfoContext(ctx, "Сохраняем операцию в Google Таблицу",
		slog.Int64("user_id", user.TelegramID),
		slog.String("spreadsheet_id", user.SpreadsheetID),
		slog.Int("transaction_id", nextTransactionID),
	)

	transaction := aiTransactionData.ToTransaction(int64(nextTransactionID), user.TelegramID)

	if err = r.sheetsService.SaveTransaction(ctx, user.SpreadsheetID, transaction); err != nil {
		return err
	}

	// Send result message
	textResponse, menu := basicTransactionMarkup(transaction, user)

	// Delete previous clarification notification
	_ = r.bot.Delete(c.Message())

	// Delete inline clarification buttons
	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

	sentMessage, err := r.bot.Send(c.Chat(), textResponse, menu, telebot.ModeHTML)
	if err != nil {
		return err
	}

	// Cleaning inline editing buttons
	if user.LastMessageID > 0 {
		// Creating message for telebot
		prevMsg := &telebot.Message{
			ID:   user.LastMessageID,
			Chat: &telebot.Chat{ID: user.TelegramID},
		}
		// Removing inline buttons for previous message
		_, _ = r.bot.EditReplyMarkup(prevMsg, nil)
	}

	// Updating user (reseting state, saving last sent message id)
	slog.InfoContext(
		ctx,
		"Очистка pending-транзакции и установка состояния Ready",
		slog.Int64("user_id", user.TelegramID),
	)

	user.PendingTransaction = ""
	user.State = domain.StateReady
	user.LastMessageID = sentMessage.ID

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	return nil
}

func (r *Router) handleCancelTransactionClarification(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to user
	_ = c.Respond()

	// Getting user
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	user.State = domain.StateReady
	user.PendingTransaction = ""
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
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

func (r *Router) sendUserCategoriesForEditing(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)
	transactionIDStr := c.Data()
	transactionID, err := strconv.Atoi(transactionIDStr)
	if err != nil {
		return fmt.Errorf("invalid transaction id in callback data (%s): %w", transactionIDStr, err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Unmarshal user categories
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return fmt.Errorf("failed to unmarshal user categories: %w", err)
	}

	// Searching for an actual transaction row in sheets
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		return err
	}

	categoriesMarkup := transactionCategoriesMarkup(transactionID, categories)
	text := fmt.Sprintf(
		"✏️ <b>Редактирование операции #%d</b>\n\n"+
			"• Текущая выбранная категория: <b>%s</b>\n\n"+
			"📝 Выбери новую категорию:",
		transactionRow.Transaction.ID,
		transactionRow.Transaction.Category,
	)

	// Если переходим по клику на кнопку — лучше обновить сообщение через c.Edit,
	// чтобы не плодить новые сообщения в чате:
	if c.Callback() != nil {
		return c.Edit(text, categoriesMarkup, telebot.ModeHTML)
	}

	return c.Send(text, categoriesMarkup, telebot.ModeHTML)
}

func (r *Router) changeTransactionCategory(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)
	_ = c.Respond()

	// Getting category and transaction id
	payload := c.Data()
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid callback data format: %s", payload)
	}

	// Extracting transaction id
	transactionID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid transaction id in callback data (%s): %w", parts[0], err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Unmarshaling user categories to resolve category by index
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return fmt.Errorf("failed to unmarshal user categories: %w", err)
	}

	catIdx, err := strconv.Atoi(parts[1])
	if err != nil || catIdx < 0 || catIdx >= len(categories) {
		return fmt.Errorf("invalid category index in callback data (%s)", parts[1])
	}
	selectedCategory := categories[catIdx]

	// Updating transaction category in sheets
	err = r.sheetsService.UpdateTransactionCategory(ctx, user.SpreadsheetID, transactionID, selectedCategory)
	if err != nil {
		return err
	}

	// Getting updated transaction data from sheet
	transaction, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, int(transactionID))
	if err != nil {
		slog.WarnContext(ctx, "Не удалось перечитать операцию после смены категории",
			slog.Int64("tx_id", transactionID),
			slog.Any("error", err),
		)

		fallbackText := fmt.Sprintf("✅ Категория операции #%d изменена на <b>%s</b>!", transactionID, selectedCategory)
		return c.Edit(fallbackText, telebot.ModeHTML)
	}

	// Send result message
	textResponse, menu := basicTransactionMarkup(transaction.Transaction, user)

	return c.Edit(textResponse, menu, telebot.ModeHTML)
}
