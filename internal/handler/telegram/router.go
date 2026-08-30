package telegram

import (
	"fmt"

	"em-finance-bot/config"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot *telebot.Bot
	cfg *config.Config
}

func NewRouter(bot *telebot.Bot, cfg *config.Config) *Router {
	return &Router{
		bot: bot,
		cfg: cfg,
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
	btnCopyTemplate := step1Markup.URL("📑 1. Скопировать шаблон таблицы", r.cfg.TemplateSheetURL)
	btnConfirmAccess := step1Markup.Data("✅ 2. Я открыл доступ боту", "step_confirm_access")
	step1Markup.Inline(
		step1Markup.Row(btnCopyTemplate),
		step1Markup.Row(btnConfirmAccess),
	)

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
				"⚙️ *Быстрая настройка за 2 шага:*\n\n"+
				"1️⃣ Нажми кнопку ниже и создай копию шаблона таблицы.\n"+
				"2️⃣ В новой таблице нажми *«Настройки доступа» (Share)* и добавь сервисный email редактором:\n`%s`\n\n"+
				"Как только откроешь доступ — нажимай кнопку подтверждения 👇",
			user.FirstName,
			r.cfg.GoogleServiceAccountEmail,
		)

		return c.Send(text, step1Markup, telebot.ModeMarkdown)
	})
}