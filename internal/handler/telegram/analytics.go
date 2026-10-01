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
	ctx := c.Get(ContextKey).(context.Context)
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}
	if user.SpreadsheetID == "" {
		return domain.ErrSheetNotConfigured
	}

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
	var inputText string

	if c.Message().Voice == nil {
		inputText = strings.TrimSpace(c.Text())
	} else {
		waitVoiceMsg, _ := r.bot.Send(c.Chat(), "🎙 Слушаю голосовое...")
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

		transcription, err := r.aiService.TranscribeVoice(ctx, voiceBytes)
		if waitVoiceMsg != nil {
			_ = r.bot.Delete(waitVoiceMsg)
		}

		if err != nil || strings.TrimSpace(transcription) == "" {
			return domain.ErrVoiceTranscriptionFailed
		}
		inputText = strings.TrimSpace(transcription)
	}

	if inputText == "" {
		return domain.ErrEmptyTransaction
	}

	if err := r.runFinancialAnalytics(ctx, c, user, inputText); err != nil {
		return err
	}

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

	// 1. Извлечение дат через первый промпт
	dateQuery, err := r.aiService.ExtractDateFilter(ctx, queryText, user.Timezone, user.CategoriesCache)
	if err != nil {
		slog.WarnContext(ctx, "Не удалось распознать период", slog.Any("error", err))
		return c.Send("Не удалось распознать период. Попробуйте написать: <i>«Расходы за прошлую неделю»</i>", telebot.ModeHTML)
	}

	slog.InfoContext(ctx, "Диапазон дат распознан",
		slog.String("start_date", dateQuery.StartDate),
		slog.String("end_date", dateQuery.EndDate),
		slog.String("category", dateQuery.Category),
		slog.String("period_label", dateQuery.PeriodLabel),
	)

	// 2. Чтение E3:J из Google Таблицы и фильтрация в Go
	rows, err := r.sheetsService.FetchAllTransactions(ctx, user.SpreadsheetID)
	if err != nil {
		slog.ErrorContext(ctx, "Ошибка при чтении данных из Google Таблицы", slog.Any("error", err))
		return c.Send("❌ Ошибка при чтении данных из Google Таблицы.")
	}

	filtered := ai.FilterTransactionsByDate(rows, dateQuery.StartDate, dateQuery.EndDate, dateQuery.Category)
	slog.InfoContext(ctx, "Результат фильтрации транзакций",
		slog.Int("total_rows", len(rows)),
		slog.Int("filtered_rows", len(filtered)),
		slog.String("start_date", dateQuery.StartDate),
		slog.String("end_date", dateQuery.EndDate),
		slog.String("category", dateQuery.Category),
	)

	// 3. Агрегация сумм в Go (экономит токены и гарантирует точную математику)
	summaryData := ai.AggregateTransactions(filtered, user.Currency)

	// 4. Генерация текста через второй аналитический промпт
	periodLabel := dateQuery.PeriodLabel
	if dateQuery.Category != "" && !strings.Contains(strings.ToLower(periodLabel), strings.ToLower(dateQuery.Category)) {
		periodLabel = fmt.Sprintf("%s (Категория: %s)", periodLabel, dateQuery.Category)
	}

	analysisHTML, err := r.aiService.GenerateFinancialReport(ctx, periodLabel, summaryData)
	if err != nil {
		slog.ErrorContext(ctx, "Не удалось сформировать отчет", slog.Any("error", err))
		return c.Send("Не удалось сформировать отчет.")
	}

	if err := c.Send(analysisHTML, telebot.ModeHTML); err != nil {
		slog.WarnContext(ctx, "Не удалось отправить отчет в ModeHTML, отправляем обычным текстом", slog.Any("error", err))
		return c.Send(analysisHTML)
	}

	return nil
}
