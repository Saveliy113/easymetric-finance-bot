package telegram

import (
	"context"
	"fmt"

	"em-finance-bot/config"
	"em-finance-bot/internal/domain"
	db "em-finance-bot/internal/repository/sqlite"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot      *telebot.Bot
	cfg      *config.Config
	userRepo *db.UserRepository
}

func NewRouter(bot *telebot.Bot, cfg *config.Config, userRepo *db.UserRepository) *Router {
	return &Router{
		bot:      bot,
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// Telegram comands and events registration
func (r *Router) Register() {
	// Main menu and inline buttons
	menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
	btnCategories := menu.Text("⚙️ Категории")
	menu.Reply(menu.Row(btnCategories))

	// Inline buttons for first step
	step1Markup := &telebot.ReplyMarkup{}
	btnStartConfiguration := step1Markup.Data("⚙️ Начать настройку", "start_configuration")
	step1Markup.Inline(
		step1Markup.Row(btnStartConfiguration),
	)

	// Handling configuration first step
	r.bot.Handle(&btnStartConfiguration, func(c telebot.Context) error {
		// Responding to telegram to stop loading animation
		_ = c.Respond()

		// Delete inline buttons from the previous message
		_, _ = r.bot.EditReplyMarkup(c.Message(), nil)

		// Save user in the db
		ctx := context.Background()
		sender := c.Sender()

		user := &domain.User{
			TelegramID: sender.ID,
			Username:   sender.Username,
			State:      domain.StateAwaitingCity,
		}

		if err := r.userRepo.Upsert(ctx, user); err != nil {
			// TODO: Log the error and return back "Go to configure" button
			return c.Send("⚠️ Произошла ошибка при сохранении профиля. Попробуй еще раз.")
		}

		// Sending next step
		return c.Send(
			"📍 *Шаг 1 из 3: Твой город*\n\n"+
				"Напиши свой город (например, Алматы или Москва). Это нужно для точного времени и базовой валюты:",
			telebot.ModeMarkdown,
		)
	})

	// Inline buttons for step 2 - categories choice
	categoriesChoiceMarkup := &telebot.ReplyMarkup{}
	btnKeepDefault := categoriesChoiceMarkup.Data("👍 Оставить базовые категории", "cat_keep_default")
	btnCustomCat := categoriesChoiceMarkup.Data("✏️ Настроить свои категории", "cat_custom")
	categoriesChoiceMarkup.Inline(
		categoriesChoiceMarkup.Row(btnKeepDefault),
		categoriesChoiceMarkup.Row(btnCustomCat),
	)

	// Обработчик команды /start
	r.bot.Handle("/start", func(c telebot.Context) error {
		user := c.Sender()

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

		return c.Send(text, step1Markup, telebot.ModeMarkdown)
	})
}
