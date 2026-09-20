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
		slog.DebugContext(ctx, "Запрос отменен клиентом или прерван по таймауту", slog.Any("error", err))
		return nil
	}

	// User metadata for logging (if available)
	var userID int64
	var username string
	if sender := c.Sender(); sender != nil {
		userID = sender.ID
		username = sender.Username
	}

	// Handling business errors (Known Domain/Service Errors)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		slog.WarnContext(ctx, "Пользователь не найден в системе",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Похоже, что ты еще не зарегистрирован. Отправь /start для начала работы.")
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

	return c.Send(userMsg, telebot.ModeHTML)
}
