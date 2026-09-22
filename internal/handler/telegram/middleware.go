package telegram

import (
	"context"

	"em-finance-bot/pkg/idgen"
	"em-finance-bot/pkg/trace"

	"gopkg.in/telebot.v3"
)

const ContextKey = "ctx"

func (r *Router) TelemetryMiddleware() telebot.MiddlewareFunc {
	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			// 1. Create trace ID and Context
			traceID := idgen.Short()
			ctx := trace.WithId(context.Background(), traceID)

			// 2. Attach context to telebot.Context for handlers to consume
			c.Set(ContextKey, ctx)

			// 3. Execute handler; catch any error and delegate directly to handleError
			if err := next(c); err != nil {
				return r.handleError(ctx, c, err)
			}

			return nil
		}
	}
}
