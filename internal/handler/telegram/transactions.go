package telegram

import (
	"context"
	"em-finance-bot/internal/service/ai"
	"em-finance-bot/internal/service/sheets"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMoneyOperation(c telebot.Context) error {
	ctx := context.Background()
	senderId := c.Sender().ID

	// Detect the message type: text or speach and get message text
	var inputText string

	if c.Message().Voice == nil {
		inputText = strings.TrimSpace(c.Text())
	} else {
		waitVoiceMsg, _ := r.bot.Send(c.Chat(), "🎙 Слушаю голосовое...")

		// Downloading audio file from tg
		voiceFile, err := r.bot.File(&c.Message().Voice.File)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}

			return c.Send("⚠️ Не удалось загрузить голосовое сообщение. Попробуй ещё раз.")
		}

		defer voiceFile.Close()

		voiceBytes, err := io.ReadAll(voiceFile)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}
			return c.Send("⚠️ Ошибка при чтении аудиофайла.")
		}

		// Getting transcription with Gemini
		transcription, err := r.aiService.TranscribeVoice(ctx, voiceBytes)
		if waitVoiceMsg != nil {
			_ = r.bot.Delete(waitVoiceMsg)
		}

		if err != nil || strings.TrimSpace(transcription) == "" {
			return c.Send("⚠️ Не удалось разобрать слова в голосовом. Попробуй написать текстом.")
		}

		inputText = strings.TrimSpace(transcription)
	}

	if inputText == "" {
		return c.Send("⚠️ Не удалось распознать сообщение. Попробуй ещё раз.")
	}

	// Getting user data from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении данных пользователя")
	}

	// Deserializing user expenses categories
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return c.Send("⚠️ Ошибка при обработке данных по категориям пользователя")
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Обрабатываю операцию...")

	// Pass all information to ai service
	transaction, err := r.aiService.ParsedTransaction(ctx, inputText, categories, user.Currency, user.Timezone)
	if err != nil || !transaction.IsValid {
		fmt.Println("ERROR PARSING TRANSACTION", err)
		return c.Send("⚠️ Не удалось распознать операцию или сумму.\nПример: `Такси 1200` или `Зарплата 350000`", telebot.ModeMarkdown)
	}

	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	// Handling transaction clarification
	// if transaction.NeedsClarification && len(transaction.SuggestedCategories) > 0 {
	// 	// Сохраняем черновик транзакции во временное поле пользователя
	// 	txBytes, _ := json.Marshal(transaction)
	// 	user.PendingTransaction = string(txBytes)
	// 	user.State = domain.StateAwaitingCategoryClarification
	// 	_ = r.userRepo.Upsert(ctx, user)

	// 	// Формируем кнопки с предложенными вариантами
	// 	clarifyMarkup := &telebot.ReplyMarkup{}
	// 	var rows []telebot.Row
	// 	for _, cat := range transaction.SuggestedCategories {
	// 		btn := clarifyMarkup.Data(cat, "set_cat", cat)
	// 		rows = append(rows, clarifyMarkup.Row(btn))
	// 	}
	// 	clarifyMarkup.Inline(rows...)

	// 	prompt := fmt.Sprintf(
	// 		"🤔 Нашел операцию: *%s* на сумму `%.2f %s`, но сомневаюсь в категории.\n\nВыбери подходящую категорию:",
	// 		transaction.Description,
	// 		transaction.Amount,
	// 		user.Currency,
	// 	)
	// 	return c.Send(prompt, clarifyMarkup, telebot.ModeMarkdown)
	// }

	// Saving operation to Google sheets
	parsedDate, err := ai.ParseTransactionDate(transaction.Date, user.Timezone)
	if err != nil {
		slog.Error("Не удалось разобрать дату транзакции", "дата", transaction.Date, "таймзона", user.Timezone, "ошибка", err)
		return c.Send("⚠️ Не удалось распознать дату операции. Попробуй ещё раз.")
	}

	if err = r.sheetsService.SaveTransaction(ctx, user.SpreadsheetID, &sheets.Transaction{
		UserID:      user.TelegramID,
		Type:        sheets.TransactionType(transaction.Type), // "expense" или "income"
		Amount:      transaction.Amount,
		Category:    transaction.Category,
		Description: transaction.Description,
		Date:        parsedDate,
		CreatedAt:   time.Now(),
	}); err != nil {
		slog.Error("Не удалось сохранить операцию в Google Таблицу", "ошибка", err, "user_id", user.TelegramID)
		return c.Send("⚠️ Произошла ошибка при записи в таблицу. Попробуй позже.")
	}

	var textResponse string
	if transaction.Type == string(sheets.TypeIncome) { //TODO: fix type
		textResponse = fmt.Sprintf("✅ <b>Доход записан!</b>\n\n💰 Сумма: <b>%.2f</b>\n📝 Описание: %s\n📅 Дата: %s",
			transaction.Amount,
			transaction.Description,
			parsedDate.Format("02.01.2006"),
		)
	} else {
		textResponse = fmt.Sprintf("✅ <b>Расход записан!</b>\n\n💸 Сумма: <b>%.2f</b>\n📁 Категория: <b>%s</b>\n📝 Описание: %s\n📅 Дата: %s",
			transaction.Amount,
			transaction.Category,
			transaction.Description,
			parsedDate.Format("02.01.2006"),
		)
	}

	return c.Send(textResponse, telebot.ModeHTML)
}
