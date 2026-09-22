package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"em-finance-bot/pkg/trace"
	"errors"
	"fmt"
	"log/slog"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleError(ctx context.Context, c telebot.Context, err error) error {
	// Ignoring request cancelation by user or due to timeout
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		slog.InfoContext(ctx, "Запрос отменен клиентом или прерван по таймауту", slog.Any("error", err))
		return nil
	}

	// User metadata for logging (if available)
	var userID int64
	var username string
	if sender := c.Sender(); sender != nil {
		userID = sender.ID
		username = sender.Username
	}

	// Telegram API Transport Errors (e.g., bot blocked by user)
	var tgErr *telebot.Error
	if errors.As(err, &tgErr) {
		if tgErr.Code == 403 {
			slog.InfoContext(ctx, "Bot blocked by user", slog.Int64("user_id", userID))
			return nil
		}
		slog.WarnContext(ctx, "Telegram API transport error",
			slog.Int64("user_id", userID),
			slog.Int("code", tgErr.Code),
			slog.String("description", tgErr.Description),
		)
		return nil
	}

	// Handling business errors (Known Domain/Service Errors)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		slog.WarnContext(ctx, "Пользователь не найден в системе",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Похоже, что ты еще не зарегистрирован. Отправь /start для начала работы.")
	case errors.Is(err, domain.ErrInvalidCity):
		slog.WarnContext(ctx, "Некорректный или пустой город",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Пожалуйста, напиши корректное название города. Например, Алматы, Астана, Москва:")
	case errors.Is(err, domain.ErrParsingCity):
		slog.WarnContext(ctx, "Ошибка при определении часового пояса и города",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Не удалось распознать город 😔\nПроверь корректность названия и попробуй написать ещё раз:")
	case errors.Is(err, domain.ErrInvalidCategories):
		slog.WarnContext(ctx, "Некорректный список категорий",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Пожалуйста, отправь список категорий через запятую. Например: Продукты, Кафе, Транспорт, Развлечения")
	case errors.Is(err, domain.ErrParsingCategories):
		slog.WarnContext(ctx, "Ошибка при обработке категорий в Gemini",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Не удалось распознать категории 😔\nПопробуй написать ещё раз через запятую:")
	case errors.Is(err, domain.ErrInvalidSheetURL):
		slog.WarnContext(ctx, "Некорректная ссылка на Google Таблицу",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось извлечь ID таблицы. Убедись, что отправляешь корректную ссылку на Google Таблицу:")
	case errors.Is(err, domain.ErrSheetAccessDenied):
		slog.WarnContext(ctx, "Ошибка доступа к Google Таблице",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		msg := fmt.Sprintf(
			"⚠️ *Не удалось получить доступ к таблице!*\n\n"+
				"Убедись, что ты добавил сервисный аккаунт с правами *Редактора*:\n`%s`\n\n"+
				"После этого отправь ссылку еще раз.",
			r.cfg.GoogleServiceAccountEmail,
		)
		return c.Send(msg, telebot.ModeMarkdown)
	case errors.Is(err, domain.ErrSheetNotConfigured):
		slog.WarnContext(ctx, "Google Таблица не настроена",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Google Таблица ещё не подключена. Пожалуйста, заверши настройку с помощью команды /start.")
	case errors.Is(err, domain.ErrVoiceDownloadFailed):
		slog.WarnContext(ctx, "Не удалось загрузить голосовое сообщение",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось загрузить голосовое сообщение. Попробуй ещё раз или отправь текстом.")
	case errors.Is(err, domain.ErrVoiceTranscriptionFailed):
		slog.WarnContext(ctx, "Не удалось расшифровать голосовое сообщение",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось разобрать слова в голосовом сообщении. Попробуй записать чётче или написать текстом.")
	case errors.Is(err, domain.ErrEmptyTransaction):
		slog.WarnContext(ctx, "Пустой текст финансовой операции",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось распознать сообщение. Попробуй ещё раз.")
	case errors.Is(err, domain.ErrInvalidTransaction):
		slog.WarnContext(ctx, "Не удалось распознать финансовую операцию",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось распознать операцию или сумму.\nПример: `Такси 1200` или `Зарплата 350000`", telebot.ModeMarkdown)
	case errors.Is(err, domain.ErrInvalidTransactionDate):
		slog.WarnContext(ctx, "Ошибка разбора даты операции",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("⚠️ Не удалось определить дату операции. Попробуй ещё раз.")
	}

	// Handling technical errors
	traceID := trace.FromContext(ctx)

	slog.ErrorContext(ctx, "Внутренний системный сбой",
		slog.Int64("user_id", userID),
		slog.String("username", username),
		slog.Any("error", err),
	)

	userMsg := fmt.Sprintf(
		"⚠️ <b>Произошла внутренняя ошибка.</b>\n\n"+
			"Попробуй ещё раз позже. Если проблема не решится, перешли это разработчику <a href=\"https://t.me/saveliy_d13\">@saveliy_d13</a>:\n\n"+
			"<code>Код ошибки: %s</code>",
		traceID,
	)

	if sendErr := c.Send(userMsg, telebot.ModeHTML); sendErr != nil {
		slog.ErrorContext(ctx, "Failed to deliver error message to user",
			slog.Int64("user_id", userID),
			slog.Any("error", sendErr),
		)
		return sendErr
	}

	return nil
}

func CatchUnhandledErrors(err error, c telebot.Context) {
	traceID := "none"
	ctx := context.Background()
	if c != nil {
		if reqCtx, ok := c.Get(ContextKey).(context.Context); ok {
			ctx = reqCtx
			traceID = trace.FromContext(ctx)
		}
	}
	slog.ErrorContext(ctx, "Unhandled Telegram update error",
		slog.String("trace_id", traceID),
		slog.Any("error", err),
	)
}
