package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"em-finance-bot/internal/domain"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleGreeting(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)
	sender := c.Sender()

	slog.InfoContext(ctx, "Команда /start",
		slog.Int64("user_id", sender.ID),
		slog.String("username", sender.Username),
	)

	startConfigMarkup := r.startConfigMarkup()

	text := fmt.Sprintf(
		"👋 Привет, %s!\n\n"+
			"Я твой личный финансовый ассистент. Помогу легко вести учет доходов и расходов без рутины и лишних усилий.\n\n"+
			"🔒 *Полная приватность:* все данные хранятся исключительно в твоей личной Google Таблице — доступ к ним остается только у тебя.\n\n"+
			"💡 *Как это работает:*\n"+
			"Просто отправляй мне информацию о тратах или поступлениях текстом или голосовым сообщением (например, `Кофе 1500` или `Зарплата 450000`). Я сам распознаю детали, определю категорию и внесу запись в таблицу.\n\n"+
			"📊 *Аналитика в один клик:*\n"+
			"Ты всегда можешь спросить: _«Сколько я потратил на кофе в апреле?»_ или запросить полную статистику за любой период.\n\n"+
			"⚙️ *Перед тем, как начать, нужно выполнить простую настройку* 👇\n\n",
		sender.FirstName,
	)

	return c.Send(text, startConfigMarkup, telebot.ModeMarkdown)
}

func (r *Router) handleStartConfiguration(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)
	sender := c.Sender()

	slog.InfoContext(ctx, "Начало настройки профиля",
		slog.Int64("user_id", sender.ID),
		slog.String("username", sender.Username),
	)

	// Responding to telegram to stop loading animation
	_ = c.Respond()

	// Delete inline buttons from the previous message
	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

	// Check if user already exists and is fully configured
	existingUser, err := r.userRepo.GetByTelegramId(ctx, sender.ID)
	if err == nil && existingUser != nil {
		if existingUser.State == domain.StateReady && existingUser.SpreadsheetID != "" {
			// User is already registered and configured — redirect to main menu
			slog.InfoContext(ctx, "Пользователь уже зарегистрирован, перенаправляем в главное меню",
				slog.Int64("user_id", sender.ID),
			)
			return c.Send(
				"👋 Ты уже зарегистрирован и настроен!\n\n"+
					"Используй кнопку *Меню* слева от поля ввода для быстрого доступа к отчетам, таблице и настройкам.",
				telebot.ModeMarkdown,
				r.menuUI.ReplyMenu,
			)
		}

		// User exists but stuck in mid-onboarding — resume from current step
		slog.InfoContext(ctx, "Пользователь в процессе настройки, продолжаем с текущего шага",
			slog.Int64("user_id", sender.ID),
			slog.String("state", string(existingUser.State)),
		)
		return r.resumeOnboarding(ctx, c, existingUser)
	}

	// New user — proceed with fresh registration
	user := &domain.User{
		TelegramID: sender.ID,
		Username:   sender.Username,
		State:      domain.StateAwaitingCity,
	}

	slog.InfoContext(ctx, "Создаем профиль нового пользователя в БД",
		slog.Int64("user_id", sender.ID),
		slog.String("state", string(user.State)),
	)

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	return r.handleCityStep(ctx, c)
}

// resumeOnboarding sends the user to the onboarding step they're currently on
func (r *Router) resumeOnboarding(ctx context.Context, c telebot.Context, user *domain.User) error {
	switch user.State {
	case domain.StateAwaitingCity:
		return r.handleCityStep(ctx, c)
	case domain.StateAwaitingCategories:
		return r.handleCategoriesStep(ctx, c)
	case domain.StateAwaitingSheetURL:
		return r.handleSheetStep(ctx, c)
	default:
		// Unknown or intermediate state — reset to city step
		user.State = domain.StateAwaitingCity
		_ = r.userRepo.Upsert(ctx, user)
		return r.handleCityStep(ctx, c)
	}
}

func (r *Router) handleCityStep(ctx context.Context, c telebot.Context) error {
	slog.InfoContext(ctx, "Отправка шага указания города",
		slog.Int64("user_id", c.Sender().ID),
	)

	return c.Send(
		"📍 *Шаг 1 из 3: Твой город*\n\n"+
			"Напиши свой город (например, Алматы или Москва). Это нужно для точного времени и базовой валюты:",
		telebot.ModeMarkdown,
	)
}

