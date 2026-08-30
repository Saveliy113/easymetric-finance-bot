package telegram

import (
	"fmt"
	"log"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot *telebot.Bot
}

func NewRouter(bot *telebot.Bot) *Router {
	return &Router{
		bot: bot,
	}
}

// Telegram comands and events registration
func (r *Router) Register() {
	// Создание постоянной клавиатуры меню
	menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
	btnCategories := menu.Text("⚙️ Категории")
	btnHelp := menu.Text("ℹ️ Помощь")
	menu.Reply(menu.Row(btnCategories, btnHelp))
	
	// Обработчик команды /start
	r.bot.Handle("/start", func(c telebot.Context) error {
		user := c.Sender()
		log.Printf("[START] Новый пользователь: ID=%d, @%s (%s)", user.ID, user.Username, user.FirstName)

		msg := fmt.Sprintf(
			"👋 Привет, *%s*!\n\n"+
				"Я бот *@easymetric_finance_bot*.\n"+
				"Сервер на Go + Fiber успешно запущен и слушает вебхук.\n\n"+
				"Отправь мне трату (например: `Кофе 1500`) или выбери действие в меню ниже.",
			user.FirstName,
		)

		return c.Send(msg, menu, telebot.ModeMarkdown)
	})
}