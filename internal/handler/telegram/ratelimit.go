package telegram

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"gopkg.in/telebot.v3"
)

type userLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// UserRateLimiter manages in-memory rate limiters per Telegram user ID with automatic cleanup
type UserRateLimiter struct {
	mu          sync.Mutex
	limiters    map[int64]*userLimiterEntry
	rate        rate.Limit
	burst       int
	cleanupTTL  time.Duration
}

// NewUserRateLimiter creates a new UserRateLimiter.
// Default: 1 token every 1.5 seconds, burst of 6 tokens, 30 min idle cleanup.
func NewUserRateLimiter(r rate.Limit, burst int, cleanupTTL time.Duration) *UserRateLimiter {
	limiter := &UserRateLimiter{
		limiters:   make(map[int64]*userLimiterEntry),
		rate:       r,
		burst:      burst,
		cleanupTTL: cleanupTTL,
	}

	// Start periodic cleanup of inactive limiters to avoid memory leak
	go limiter.startPeriodicCleanup(10 * time.Minute)

	return limiter
}

func (u *UserRateLimiter) Allow(userID int64) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	now := time.Now()
	entry, exists := u.limiters[userID]
	if !exists {
		entry = &userLimiterEntry{
			limiter: rate.NewLimiter(u.rate, u.burst),
		}
		u.limiters[userID] = entry
	}
	entry.lastSeen = now

	return entry.limiter.Allow()
}

func (u *UserRateLimiter) Cleanup(olderThan time.Duration) int {
	u.mu.Lock()
	defer u.mu.Unlock()

	now := time.Now()
	removed := 0
	for id, entry := range u.limiters {
		if now.Sub(entry.lastSeen) > olderThan {
			delete(u.limiters, id)
			removed++
		}
	}
	return removed
}

func (u *UserRateLimiter) startPeriodicCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		u.Cleanup(u.cleanupTTL)
	}
}

// RateLimitMiddleware returns a telebot middleware that limits requests per user
func (r *Router) RateLimitMiddleware() telebot.MiddlewareFunc {
	// 1 token every 1.5 seconds, burst of 6
	limiter := NewUserRateLimiter(rate.Every(1500*time.Millisecond), 6, 30*time.Minute)

	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			sender := c.Sender()
			if sender == nil {
				return next(c)
			}

			// Context with trace ID from TelemetryMiddleware
			ctx, ok := c.Get(ContextKey).(context.Context)
			if !ok {
				ctx = context.Background()
			}

			if !limiter.Allow(sender.ID) {
				slog.WarnContext(ctx, "Превышен лимит запросов для пользователя",
					slog.Int64("user_id", sender.ID),
					slog.String("username", sender.Username),
				)

				// For inline callbacks, send toast alert
				if c.Callback() != nil {
					return c.Respond(&telebot.CallbackResponse{
						Text:      "⏳ Слишком много запросов. Подожди пару секунд.",
						ShowAlert: true,
					})
				}

				// For regular messages, send text message
				return c.Send("⏳ Слишком много запросов. Пожалуйста, подожди пару секунд перед следующим действием.")
			}

			return next(c)
		}
	}
}
