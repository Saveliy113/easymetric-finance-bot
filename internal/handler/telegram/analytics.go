package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"em-finance-bot/internal/domain"
	"em-finance-bot/internal/service/ai"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMainMenuSummary(c telebot.Context) error {
	// Getting request context with trace id
	ctx := c.Get(ContextKey).(context.Context)

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Guard against unconfigured sheet
	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

	slog.InfoContext(ctx, "Запрос на формирование аналитического отчета",
		slog.Int64("user_id", user.TelegramID),
	)

	// Updating user state to await analytics dates
	user.State = domain.StateAwaitingAnalyticsDates
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	msg := "📅 <b>За какой период сформировать отчет?</b>\n\n" +
		"Напишите период текстом или отправьте голосовое сообщение.\n\n" +
		"<i>Например:</i>\n" +
		"• <code>в этом месяце</code>\n" +
		"• <code>на прошлой неделе</code>\n" +
		"• <code>сколько ушло на еду за август</code>\n" +
		"• <code>траты за последние 7 дней</code>"

	return c.Send(msg, telebot.ModeHTML)
}

func (r *Router) handleAnalyticsDatesInput(ctx context.Context, c telebot.Context, user *domain.User) error {
	// Detect the message type: text or voice and get message text
	var inputText string

	if c.Message().Voice == nil {
		inputText = strings.TrimSpace(c.Text())
		slog.InfoContext(ctx, "Получен запрос аналитики (текст)",
			slog.Int64("user_id", user.TelegramID),
			slog.String("text", inputText),
		)
	} else {
		slog.InfoContext(ctx, "Получен запрос аналитики (голосовое)",
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

	// Running financial analytics
	if err := r.runFinancialAnalytics(ctx, c, user, inputText); err != nil {
		return err
	}

	// Resetting user state back to ready
	user.State = domain.StateReady
	return r.userRepo.Upsert(ctx, user)
}

func (r *Router) runFinancialAnalytics(ctx context.Context, c telebot.Context, user *domain.User, queryText string) error {
	waitMsg, _ := r.bot.Send(c.Chat(), "🔍 Анализирую транзакции...")
	defer func() {
		if waitMsg != nil {
			_ = r.bot.Delete(waitMsg)
		}
	}()

	slog.InfoContext(ctx, "Запуск финансовой аналитики",
		slog.Int64("user_id", user.TelegramID),
		slog.String("query", queryText),
	)

	// Extracting date filter using Gemini
	dateQuery, err := r.aiService.ExtractDateFilter(ctx, queryText, user.Timezone, user.CategoriesCache)
	if err != nil || dateQuery == nil || dateQuery.StartDate == "" || dateQuery.EndDate == "" {
		slog.WarnContext(ctx, "Не удалось распознать период", slog.Any("error", err))
		return domain.ErrParsingAnalyticsPeriod
	}

	slog.InfoContext(ctx, "Диапазон дат распознан",
		slog.String("start_date", dateQuery.StartDate),
		slog.String("end_date", dateQuery.EndDate),
		slog.String("category", dateQuery.Category),
		slog.String("period_label", dateQuery.PeriodLabel),
	)

	// Fetching all transactions from Google Sheets
	rows, err := r.sheetsService.FetchAllTransactions(ctx, user.SpreadsheetID)
	if err != nil {
		return err
	}

	// Filtering transactions by date and category
	filtered := ai.FilterTransactionsByDate(rows, dateQuery.StartDate, dateQuery.EndDate, dateQuery.Category)
	slog.InfoContext(ctx, "Результат фильтрации транзакций",
		slog.Int("total_rows", len(rows)),
		slog.Int("filtered_rows", len(filtered)),
		slog.String("start_date", dateQuery.StartDate),
		slog.String("end_date", dateQuery.EndDate),
		slog.String("category", dateQuery.Category),
	)

	// Aggregating amounts in Go
	summaryData := ai.AggregateTransactions(filtered, user.Currency)

	// Generating financial report with Gemini
	periodLabel := dateQuery.PeriodLabel
	if dateQuery.Category != "" && !strings.Contains(strings.ToLower(periodLabel), strings.ToLower(dateQuery.Category)) {
		periodLabel = fmt.Sprintf("%s (Категория: %s)", periodLabel, dateQuery.Category)
	}

	analysisHTML, err := r.aiService.GenerateFinancialReport(ctx, periodLabel, summaryData)
	if err != nil {
		return err
	}

	// Send result message
	if err := c.Send(analysisHTML, telebot.ModeHTML, r.menuUI.ReplyMenu); err != nil {
		slog.WarnContext(ctx, "Не удалось отправить отчет в ModeHTML, отправляем обычным текстом", slog.Any("error", err))
		return c.Send(analysisHTML, r.menuUI.ReplyMenu)
	}

	return nil
}
