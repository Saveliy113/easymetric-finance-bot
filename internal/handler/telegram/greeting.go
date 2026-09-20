package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"em-finance-bot/pkg/idgen"
	"em-finance-bot/pkg/trace"
	"fmt"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleGreeting(c telebot.Context) error {
	user := c.Sender()
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
		user.FirstName,
	)

	return c.Send(text, startConfigMarkup, telebot.ModeMarkdown)
}

func (r *Router) handleStartConfiguration(c telebot.Context) error {
	// Responding to telegram to stop loading animation
	_ = c.Respond()

	// Delete inline buttons from the previous message
	_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

	// Creating request trace id and creating context with it
	traceId := idgen.Short()
	ctx := trace.WithId(context.Background(), traceId)

	// Save user in the db
	sender := c.Sender()

	user := &domain.User{
		TelegramID: sender.ID,
		Username:   sender.Username,
		State:      domain.StateAwaitingCity,
	}

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return r.handleError(ctx, c, err)
	}

	// Sending next step
	return c.Send(
		"📍 *Шаг 1 из 3: Твой город*\n\n"+
			"Напиши свой город (например, Алматы или Москва). Это нужно для точного времени и базовой валюты:",
		telebot.ModeMarkdown,
	)
}
