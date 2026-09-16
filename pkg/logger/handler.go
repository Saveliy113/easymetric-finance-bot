package logger

import (
	"context"
	"fmt"
	"log/slog"

	"em-finance-bot/pkg/trace"
)

type TraceHandler struct {
	slog.Handler
}

func NewTraceHandler(h slog.Handler) *TraceHandler {
	return &TraceHandler{Handler: h}
}

func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if traceID := trace.FromContext(ctx); traceID != "" {
		r.Message = fmt.Sprintf("[%s] %s", traceID, r.Message)
	}
	return h.Handler.Handle(ctx, r)
}
