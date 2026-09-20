package telegram

import (
	"context"
	"em-finance-bot/internal/domain"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleSheetURLInput(c telebot.Context) error {
	ctx := context.Background()
	senderId := c.Sender().ID
	sheetUrl := strings.TrimSpace(c.Text())

	// Extracting unique sheet id from url
	sheetIDRegex := regexp.MustCompile(`/d/([a-zA-Z0-9_-]+)`)
	matches := sheetIDRegex.FindStringSubmatch(sheetUrl)

	if len(matches) < 2 {
		return c.Send("⚠️ Не удалось извлечь ID таблицы. Попробуй другую ссылку.")
	}

	sheetID := matches[1]

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Проверяю доступ к таблице...")

	// Checking the bot is able to operate with the table
	err := r.sheetsService.ValidateAccess(ctx, sheetID)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		errorMsg := fmt.Sprintf(
			"⚠️ *Не удалось получить доступ к таблице!*\n\n"+
				"Причина: %s\n\n"+
				"Убедись, что ты добавил сервисный аккаунт с правами *Редактора*:\n`%s`\n\n"+
				"После этого отправь ссылку еще раз.",
			err.Error(),
			r.cfg.GoogleServiceAccountEmail,
		)
		return c.Send(errorMsg, telebot.ModeMarkdown)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении профиля. Попробуй позже.")
	}
	if user == nil {
		return c.Send("⚠️ Профиль не найден. Начни с команды /start.")
	}

	// Saving sheet id to the db
	user.SpreadsheetID = sheetID
	user.State = domain.StateReady

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Ошибка при сохранении таблицы. Попробуй позже.")
	}

	// Sending welcome message
	welcomeMessage := fmt.Sprintf(
		"✅ *Отлично! Твоя персональная финансовая система готова к работе.*\n\n"+
			"🔗 *Таблица:* %s\n\n"+
			" теперь ты можешь отправлять мне свои финансовые операции в свободной форме:\n"+
			"\nПримеры:\n\n"+
			"• *\"Купил кофе за 300 рублей\"*\n"+
			"• *\"Обед 850\"*\n"+
			"• *\"Пополнил баланс на 1000\"*\n\n"+
			"Каждую операцию я буду автоматически записывать в твою таблицу, классифицировать по категориям и обновлять все необходимые расчеты.\n\n"+
			"Я постараюсь максимально точно определить категории для твоих трат, но при необходимости всегда смогу задать уточняющие вопросы.\n\n"+
			"Для быстрого доступа ко всем функциям используй меню внизу.",
		sheetUrl,
	)

	return c.Send(welcomeMessage, telebot.ModeMarkdown)
}
