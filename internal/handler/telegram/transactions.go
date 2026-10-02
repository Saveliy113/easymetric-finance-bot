package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"em-finance-bot/internal/domain"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMoneyOperation(ctx context.Context, c telebot.Context, user *domain.User) error {
	// Guard against unconfigured sheet
	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	// Detect the message type: text or voice and get message text
	var inputText string

	if c.Message().Voice == nil {
		inputText = strings.TrimSpace(c.Text())
		slog.InfoContext(ctx, "Получена финансовая операция (текст)",
			slog.Int64("user_id", user.TelegramID),
			slog.String("text", inputText),
		)
	} else {
		slog.InfoContext(ctx, "Получена финансовая операция (голосовое)",
			slog.Int64("user_id", user.TelegramID),
		)
		waitVoiceMsg, _ := r.bot.Send(c.Chat(), "🎙 Слушаю голосовое...")
		_ = c.Notify(telebot.Typing)

		// Downloading audio file from tg
		voiceFile, err := r.bot.File(&c.Message().Voice.File)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}
			return domain.ErrVoiceDownloadFailed
		}
		defer voiceFile.Close()

		voiceBytes, err := io.ReadAll(voiceFile)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}
			return domain.ErrVoiceDownloadFailed
		}

		// Getting transcription with Gemini
		slog.InfoContext(ctx, "Отправляем голосовое на расшифровку в Gemini",
			slog.Int64("user_id", user.TelegramID),
		)
		transcription, err := r.aiService.TranscribeVoice(ctx, voiceBytes)
		if waitVoiceMsg != nil {
			_ = r.bot.Delete(waitVoiceMsg)
		}

		if err != nil || strings.TrimSpace(transcription) == "" {
			return domain.ErrVoiceTranscriptionFailed
		}

		inputText = strings.TrimSpace(transcription)
		slog.InfoContext(ctx, "Голос успешно расшифрован",
			slog.Int64("user_id", user.TelegramID),
			slog.String("text", inputText),
		)
	}

	if inputText == "" {
		return domain.ErrEmptyTransaction
	}

	// Deserializing user expenses categories
	var categories []string
	if user.CategoriesCache != "" {
		if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
			slog.InfoContext(ctx, "Не удалось распарсить категории пользователя из кэша",
				slog.Int64("user_id", user.TelegramID),
				slog.Any("error", err),
			)
		}
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Обрабатываю операцию...")
	_ = c.Notify(telebot.Typing)

	// Pass all information to ai service
	slog.InfoContext(ctx, "Отправляем запрос в gemini для распознавания операции",
		slog.Int64("user_id", user.TelegramID),
		slog.String("text", inputText),
	)

	aiTransactionData, err := r.aiService.ParseTransaction(ctx, inputText, categories, user.Currency, user.Timezone)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		return err
	}

	if !aiTransactionData.IsValid {
		return domain.ErrInvalidTransaction
	}

	slog.InfoContext(ctx, "Операция успешно распознана",
		slog.Int64("user_id", user.TelegramID),
		slog.String("type", aiTransactionData.Type),
		slog.Float64("amount", aiTransactionData.Amount),
		slog.String("category", aiTransactionData.Category),
		slog.String("description", aiTransactionData.Description),
	)

	slog.InfoContext(
		ctx, "Требуется уточнение категории транзации",
		slog.Bool("needs_clarification", aiTransactionData.NeedsClarification),
		slog.Float64("amount", aiTransactionData.Amount),
		slog.String("description", aiTransactionData.Description),
	)

	// If category clarification is needed,
	// sending category candidates buttons
	if aiTransactionData.NeedsClarification {
		if len(aiTransactionData.SuggestedCategories) > 0 {
			slog.InfoContext(
				ctx,
				"Отправляем варианты категорий",
			)

			return r.sendCategorySuggestions(ctx, c, aiTransactionData, user)
		} else {
			slog.InfoContext(
				ctx,
				"Отправляем форму для ручного ввода категории",
			)
			return r.sendManualCategorySelection(ctx, c, aiTransactionData, user)
		}
	}

	nextTxID, err := r.userRepo.IncrementLastTransactionID(ctx, user.TelegramID)
	if err != nil {
		return fmt.Errorf("error incrementing transaction id: %w", err)
	}

	slog.InfoContext(ctx, "Сохраняем операцию в Google Таблицу",
		slog.Int64("user_id", user.TelegramID),
		slog.String("spreadsheet_id", user.SpreadsheetID),
		slog.Int("transaction_id", nextTxID),
	)

	transaction := aiTransactionData.ToTransaction(int64(nextTxID), user.TelegramID)

	if err = r.sheetsService.SaveTransaction(ctx, user.SpreadsheetID, transaction); err != nil {
		return err
	}

	textResponse, menu := basicTransactionMarkup(transaction, user)

	sentMessage, err := r.bot.Send(c.Chat(), textResponse, menu, telebot.ModeHTML)
	if err != nil {
		return err
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
	user.LastTransactionID = nextTxID

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	return nil
}

func (r *Router) sendTransactionEditingButtons(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Getting transaction id from callback data
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

	// Searching for an actual transaction row in sheets
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		return err
	}

	// Resetting user state and draft editing reference
	user.State = domain.StateReady
	user.DraftEditTxID = 0
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	markup := transactionEditingButtonsMarkup(transactionID)
	text := fmt.Sprintf(
		"✏️ <b>Редактирование операции #%d</b>\n\n"+
			"• <b>Сумма:</b> <code>%.2f %s</code>\n"+
			"• <b>Категория:</b> %s\n"+
			"• <b>Описание:</b> %s\n"+
			"• <b>Дата:</b> <code>%s</code>\n\n"+
			"Выберите, какое поле вы хотите изменить 👇",
		transactionRow.Transaction.ID,
		transactionRow.Transaction.Amount,
		user.Currency,
		transactionRow.Transaction.Category,
		transactionRow.Transaction.Description,
		transactionRow.Transaction.Date.Format("02.01.2006 15:04"),
	)

	// Если переходим по клику на кнопку — лучше обновить сообщение через c.Edit,
	// чтобы не плодить новые сообщения в чате:
	if c.Callback() != nil {
		return c.Edit(text, markup, telebot.ModeHTML)
	}

	return c.Send(text, markup, telebot.ModeHTML)
}

func (r *Router) sendTransactionDescriptionForEditing(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to telegram to stop loading
	if c.Callback() != nil {
		_ = c.Respond()
	}

	// Getting transaction id from callback data
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
	if user == nil {
		return domain.ErrUserNotFound
	}

	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	slog.InfoContext(ctx, "Редактирование описания операции",
		slog.Int64("user_id", user.TelegramID),
		slog.Int("transaction_id", transactionID),
	)

	// Searching for an actual transaction row in sheets
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		return err
	}

	// Updating user state to await description input
	user.State = domain.StateAwaitingEditDescription
	user.DraftEditTxID = int64(transactionID)
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	msg := fmt.Sprintf(
		"✏️ <b>Редактирование описания операции #%d</b>\n\n"+
			"• <b>Текущее описание:</b> %s\n\n"+
			"Отправьте новое описание сообщением 👇",
		transactionID,
		transactionRow.Transaction.Description,
	)

	markup := cancelTransactionEditingMarkup(int64(transactionID))
	if c.Callback() != nil {
		return c.Edit(msg, markup, telebot.ModeHTML)
	}
	return c.Send(msg, markup, telebot.ModeHTML)
}

func (r *Router) handleEditTransactionDescription(ctx context.Context, c telebot.Context, user *domain.User) error {
	newDescription := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Получено новое описание операции:",
		slog.Int64("user_id", user.TelegramID),
		slog.String("description", newDescription),
	)

	// Empty string guard
	if newDescription == "" {
		return domain.ErrEmptyTransactionDescription
	}

	transactionID := user.DraftEditTxID
	if transactionID == 0 {
		user.State = domain.StateReady
		_ = r.userRepo.Upsert(ctx, user)
		return domain.ErrTransactionNotFound
	}

	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	// Updating transaction description in sheets
	err := r.sheetsService.UpdateTransactionDescription(ctx, user.SpreadsheetID, transactionID, newDescription)
	if err != nil {
		return err
	}

	// Resetting user state and draft editing reference
	user.State = domain.StateReady
	user.DraftEditTxID = 0
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Getting updated transaction data from sheet
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, int(transactionID))
	if err != nil {
		slog.WarnContext(ctx, "Не удалось перечитать операцию после смены описания",
			slog.Int64("transaction_id", transactionID),
			slog.Any("error", err),
		)
		return c.Send(fmt.Sprintf("✅ Описание операции #%d успешно обновлено!", transactionID), telebot.ModeHTML)
	}

	// Send result message
	textResponse, menu := basicTransactionMarkup(transactionRow.Transaction, user)
	return c.Send(textResponse, menu, telebot.ModeHTML)
}

func (r *Router) sendTransactionAmountForEditing(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to telegram to stop loading
	if c.Callback() != nil {
		_ = c.Respond()
	}

	// Getting transaction id from callback data
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
	if user == nil {
		return domain.ErrUserNotFound
	}

	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	slog.InfoContext(ctx, "Редактирование суммы операции",
		slog.Int64("user_id", user.TelegramID),
		slog.Int("transaction_id", transactionID),
	)

	// Searching for an actual transaction row in sheets
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		return err
	}

	// Updating user state to await amount input
	user.State = domain.StateAwaitingEditAmount
	user.DraftEditTxID = int64(transactionID)
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	msg := fmt.Sprintf(
		"💰 <b>Редактирование суммы операции #%d</b>\n\n"+
			"• <b>Текущая сумма:</b> <code>%.2f %s</code>\n\n"+
			"Отправьте новую сумму числом (например: <code>1500</code> или <code>250.50</code>) 👇",
		transactionID,
		transactionRow.Transaction.Amount,
		user.Currency,
	)

	markup := cancelTransactionEditingMarkup(int64(transactionID))
	if c.Callback() != nil {
		return c.Edit(msg, markup, telebot.ModeHTML)
	}
	return c.Send(msg, markup, telebot.ModeHTML)
}

func (r *Router) handleEditTransactionAmount(ctx context.Context, c telebot.Context, user *domain.User) error {
	amountStr := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Получена новая сумма операции:",
		slog.Int64("user_id", user.TelegramID),
		slog.String("amount", amountStr),
	)

	// Parsing amount string
	amountStr = strings.ReplaceAll(amountStr, " ", "")
	amountStr = strings.ReplaceAll(amountStr, ",", ".")

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		return domain.ErrInvalidTransactionAmount
	}

	transactionID := user.DraftEditTxID
	if transactionID == 0 {
		user.State = domain.StateReady
		_ = r.userRepo.Upsert(ctx, user)
		return domain.ErrTransactionNotFound
	}

	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	// Updating transaction amount in sheets
	err = r.sheetsService.UpdateTransactionAmount(ctx, user.SpreadsheetID, transactionID, amount)
	if err != nil {
		return err
	}

	// Resetting user state and draft editing reference
	user.State = domain.StateReady
	user.DraftEditTxID = 0
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Getting updated transaction data from sheet
	transactionRow, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, int(transactionID))
	if err != nil {
		slog.WarnContext(ctx, "Не удалось перечитать операцию после смены суммы",
			slog.Int64("transaction_id", transactionID),
			slog.Any("error", err),
		)
		return c.Send(fmt.Sprintf("✅ Сумма операции #%d успешно обновлена!", transactionID), telebot.ModeHTML)
	}

	// Send result message
	textResponse, menu := basicTransactionMarkup(transactionRow.Transaction, user)
	return c.Send(textResponse, menu, telebot.ModeHTML)
}

func (r *Router) handleDeleteTransaction(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Getting transaction id from callback data
	transactionIDStr := c.Data()
	transactionID, err := strconv.Atoi(transactionIDStr)
	if err != nil {
		_ = c.Respond()
		return fmt.Errorf("invalid transaction id in callback data (%s): %w", transactionIDStr, err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		_ = c.Respond()
		return err
	}
	if user == nil {
		_ = c.Respond()
		return domain.ErrUserNotFound
	}

	if user.SpreadsheetID == "" {
		_ = c.Respond()
		return domain.ErrSheetNotConfigured
	}

	slog.InfoContext(ctx, "Удаление транзакции по запросу пользователя",
		slog.Int64("user_id", user.TelegramID),
		slog.Int("transaction_id", transactionID),
	)

	// Deleting transaction from Google Sheets
	deletedTx, err := r.sheetsService.DeleteTransaction(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		_ = c.Respond()
		return err
	}

	// Resetting user state and draft editing reference
	user.State = domain.StateReady
	user.DraftEditTxID = 0
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		_ = c.Respond()
		return err
	}

	// Telegram popup toast notification
	_ = c.Respond(&telebot.CallbackResponse{
		Text: fmt.Sprintf("Операция #%d удалена", transactionID),
	})

	// Send result message
	textResponse := deletedTransactionText(deletedTx, user)

	return c.Edit(textResponse, &telebot.ReplyMarkup{}, telebot.ModeHTML)
}

func (r *Router) handleCancelTransactionEditing(c telebot.Context) error {
	_ = c.Respond()
	ctx := c.Get(ContextKey).(context.Context)

	// Getting transaction id from callback data
	transactionID, err := strconv.Atoi(c.Data())
	if err != nil {
		return err
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}
	if user == nil {
		return domain.ErrUserNotFound
	}

	// Searching for an actual transaction row in sheets
	found, err := r.sheetsService.FindTransactionByID(ctx, user.SpreadsheetID, transactionID)
	if err != nil {
		return domain.ErrTransactionNotFound
	}

	// Send result message
	textResponse, menu := basicTransactionMarkup(found.Transaction, user)

	// Resetting user state and draft editing reference
	user.State = domain.StateReady
	user.DraftEditTxID = 0
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	return c.Edit(textResponse, menu, telebot.ModeHTML)
}

