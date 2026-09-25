package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"em-finance-bot/internal/domain"
	"em-finance-bot/pkg/trace"

	"gopkg.in/telebot.v3"
)

type errorDescriptor struct {
	target    error
	logMsg    string
	renderMsg func(r *Router) string
	parseMode telebot.ParseMode
}

func (r *Router) getErrorDescriptors() []errorDescriptor {
	return []errorDescriptor{
		{
			target:    domain.ErrUserNotFound,
			logMsg:    "Пользователь не найден в системе",
			renderMsg: func(r *Router) string { return "Похоже, что ты еще не зарегистрирован. Отправь /start для начала работы." },
		},
		{
			target:    domain.ErrInvalidCity,
			logMsg:    "Некорректный или пустой город",
			renderMsg: func(r *Router) string { return "Пожалуйста, напиши корректное название города. Например, Алматы, Астана, Москва:" },
		},
		{
			target:    domain.ErrParsingCity,
			logMsg:    "Ошибка при определении часового пояса и города",
			renderMsg: func(r *Router) string { return "Не удалось распознать город 😔\nПроверь корректность названия и попробуй написать ещё раз:" },
		},
		{
			target:    domain.ErrInvalidCategories,
			logMsg:    "Некорректный список категорий",
			renderMsg: func(r *Router) string { return "Пожалуйста, отправь список категорий через запятую. Например: Продукты, Кафе, Транспорт, Развлечения" },
		},
		{
			target:    domain.ErrParsingCategories,
			logMsg:    "Ошибка при обработке категорий в Gemini",
			renderMsg: func(r *Router) string { return "Не удалось распознать категории 😔\nПопробуй написать ещё раз через запятую:" },
		},
		{
			target:    domain.ErrInvalidSheetURL,
			logMsg:    "Некорректная ссылка на Google Таблицу",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось извлечь ID таблицы. Убедись, что отправляешь корректную ссылку на Google Таблицу:" },
		},
		{
			target:    domain.ErrSheetNotFound,
			logMsg:    "Google Таблица не найдена",
			renderMsg: func(r *Router) string { return "⚠️ Таблица не найдена. Проверь ссылку и отправь её ещё раз:" },
		},
		{
			target: domain.ErrSheetAccessDenied,
			logMsg: "Ошибка доступа к Google Таблице",
			renderMsg: func(r *Router) string {
				return fmt.Sprintf(
					"⚠️ *Не удалось получить доступ к таблице!*\n\n"+
						"Убедись, что ты добавил сервисный аккаунт с правами *Редактора*:\n`%s`\n\n"+
						"После этого отправь ссылку еще раз.",
					r.cfg.GoogleServiceAccountEmail,
				)
			},
			parseMode: telebot.ModeMarkdown,
		},
		{
			target:    domain.ErrGoogleAPIFailed,
			logMsg:    "Ошибка доступа к Google API",
			renderMsg: func(r *Router) string { return "⚠️ Ошибка доступа к Google API. Пожалуйста, попробуй ещё раз через пару минут." },
		},
		{
			target:    domain.ErrSheetNotConfigured,
			logMsg:    "Google Таблица не настроена",
			renderMsg: func(r *Router) string { return "⚠️ Google Таблица ещё не подключена. Пожалуйста, заверши настройку с помощью команды /start." },
		},
		{
			target:    domain.ErrVoiceDownloadFailed,
			logMsg:    "Не удалось загрузить голосовое сообщение",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось загрузить голосовое сообщение. Попробуй ещё раз или отправь текстом." },
		},
		{
			target:    domain.ErrVoiceTranscriptionFailed,
			logMsg:    "Не удалось расшифровать голосовое сообщение",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось разобрать слова в голосовом сообщении. Попробуй записать чётче или написать текстом." },
		},
		{
			target:    domain.ErrEmptyTransaction,
			logMsg:    "Пустой текст финансовой операции",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось распознать сообщение. Попробуй ещё раз." },
		},
		{
			target:    domain.ErrInvalidTransaction,
			logMsg:    "Не удалось распознать финансовую операцию",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось распознать операцию или сумму.\nПример: `Такси 1200` или `Зарплата 350000`" },
			parseMode: telebot.ModeMarkdown,
		},
		{
			target:    domain.ErrInvalidTransactionDate,
			logMsg:    "Ошибка разбора даты операции",
			renderMsg: func(r *Router) string { return "⚠️ Не удалось определить дату операции. Попробуй ещё раз." },
		},
	}
}

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

	// 1. Ошибки транспорта Telegram
	var tgErr *telebot.Error
	if errors.As(err, &tgErr) {
		if tgErr.Code == 403 {
			slog.InfoContext(ctx, "Бот заблокирован пользователем", slog.Int64("user_id", userID))
			return nil
		}
		slog.InfoContext(ctx, "Транспортная ошибка Telegram API",
			slog.Int64("user_id", userID),
			slog.Int("code", tgErr.Code),
			slog.String("description", tgErr.Description),
		)
		return nil
	}

	// 2. Декларативная обработка доменных ошибок
	for _, desc := range r.getErrorDescriptors() {
		if errors.Is(err, desc.target) {
			slog.InfoContext(ctx, desc.logMsg,
				slog.Int64("user_id", userID),
				slog.String("username", username),
			)

			msg := desc.renderMsg(r)
			if desc.parseMode != "" {
				return c.Send(msg, desc.parseMode)
			}
			return c.Send(msg)
		}
	}

	// 3. Непредвиденные системные ошибки (500)
	traceID := trace.FromContext(ctx)
	slog.InfoContext(ctx, "Внутренний системный сбой",
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
		slog.InfoContext(ctx, "Не удалось доставить сообщение об ошибке пользователю",
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
	slog.InfoContext(ctx, "Необработанная ошибка Telegram",
		slog.String("trace_id", traceID),
		slog.Any("error", err),
	)
}
