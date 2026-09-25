package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"em-finance-bot/internal/domain"
	"em-finance-bot/internal/service/sheets"

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

	// Pass all information to ai service
	slog.InfoContext(ctx, "Отправляем запрос в gemini для распознавания операции",
		slog.Int64("user_id", user.TelegramID),
		slog.String("text", inputText),
	)

	transaction, err := r.aiService.ParseTransaction(ctx, inputText, categories, user.Currency, user.Timezone)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		return err
	}

	if !transaction.IsValid {
		return domain.ErrInvalidTransaction
	}

	slog.InfoContext(ctx, "Операция успешно распознана",
		slog.Int64("user_id", user.TelegramID),
		slog.String("type", transaction.Type),
		slog.Float64("amount", transaction.Amount),
		slog.String("category", transaction.Category),
		slog.String("description", transaction.Description),
	)

	slog.InfoContext(
		ctx, "Требуется уточнение категории транзации",
		slog.Bool("needs_clarification", transaction.NeedsClarification),
		slog.Float64("amount", transaction.Amount),
		slog.String("description", transaction.Description),
	)

	// If category clarification is needed,
	// sending category candidates buttons
	if transaction.NeedsClarification {
		if len(transaction.SuggestedCategories) > 0 {
			slog.InfoContext(
				ctx,
				"Отправляем варианты категорий",
			)

			return r.sendCategorySuggestions(ctx, c, transaction, user)
		} else {
			slog.InfoContext(
				ctx,
				"Отправляем форму для ручного ввода категории",
			)
			return r.sendManualCategorySelection(ctx, c, transaction, user)
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
