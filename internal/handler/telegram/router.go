package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"em-finance-bot/config"
	"em-finance-bot/internal/domain"
	db "em-finance-bot/internal/repository/sqlite"
	"em-finance-bot/internal/service/ai"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot            *telebot.Bot
	cfg            *config.Config
	userRepo       *db.UserRepository
	aiService      *ai.GeminiService
	categoriesMenu *telebot.ReplyMarkup
}

func NewRouter(bot *telebot.Bot, cfg *config.Config, userRepo *db.UserRepository, aiService *ai.GeminiService) *Router {
	return &Router{
		bot:       bot,
		cfg:       cfg,
		userRepo:  userRepo,
		aiService: aiService,
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

	// Categories markup
	r.categoriesMenu = &telebot.ReplyMarkup{}
	btnDefault := r.categoriesMenu.Data("✅ Использовать стандартные", "use_default_categories")

	r.categoriesMenu.Inline(
		r.categoriesMenu.Row(btnDefault),
	)

	// 2. Регистрируем слушатель нажатия
	r.bot.Handle(&btnDefault, r.handleUseDefaultCategories)

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

	r.bot.Handle(telebot.OnText, r.handleTextMessage)
}

func (r *Router) handleTextMessage(c telebot.Context) error {
	ctx := context.Background()
	sender := c.Sender()

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, sender.ID)
	if err != nil {
		return c.Send("⚠️ Произошла ошибка при получении профиля. Попробуй еще раз.")
	}

	if user == nil {
		// If user is not found, send a message to start the configuration
		return c.Send("⚠️ Профиль не найден. Пожалуйста, начни настройку с команды /start.")
	}

	// State based routing
	switch user.State {
	case domain.StateAwaitingCity:
		return r.handleCityInput(c, user)
	default:
		return c.Send("⚠️ Неизвестное состояние профиля. Пожалуйста, начни настройку с команды /start.")
	}
}

func (r *Router) handleCityInput(c telebot.Context, user *domain.User) error {
	ctx := context.Background()
	inputCity := strings.TrimSpace(c.Text())

	// Empty strings or too short city names guard
	if len(inputCity) < 2 {
		return c.Send("Пожалуйста, напиши корректное название города:")
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Определяю часовой пояс и валюту...")

	// Getting data using gemini
	locationInfo, err := r.aiService.ParseCity(ctx, inputCity)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil || !locationInfo.IsValid {
		return c.Send(
			"Не удалось распознать город 😔\nПопробуй написать название ещё раз (например: *Алматы*, *Москва*, *Тбилиси*):",
			telebot.ModeMarkdown,
		)
	}

	// Updating user location data and state
	user.Timezone = locationInfo.Timezone
	user.Currency = locationInfo.Currency
	user.State = domain.StateAwaitingCategories

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Ошибка при сохранении данных в базу. Попробуй ещё раз.")
	}

	// 1. Первое сообщение: подтверждение распознанных данных
	locationSummary := fmt.Sprintf(
		"✅ Город определен: *%s*\n"+
			"🕒 Часовой пояс: `%s`\n"+
			"💱 Валюта по умолчанию: `%s`",
		locationInfo.City,
		locationInfo.Timezone,
		locationInfo.Currency,
	)

	if err := c.Send(locationSummary, telebot.ModeMarkdown); err != nil {
		return err
	}

	err = r.handleCategoriesStep(c)
	if err != nil {
		return err
	}

	return nil
}

func (r *Router) handleCategoriesStep(c telebot.Context) error {
	categoriesPromptText := "📍 *Шаг 2 из 3: Настройка категорий трат*\n\n" +
		"Категории помогают боту автоматически распределять твои расходы.\n\n" +
		"Вот готовый сбалансированный набор:\n" +
		"• 🛒 *Продукты* — супермаркеты, бакалея, еда\n" +
		"• ☕ *Кафе и рестораны* — кофе, фастфуд, бары\n" +
		"• 🚗 *Транспорт* — такси, бензин, проездной\n" +
		"• 🛍 *Покупки* — одежда, техника, дом\n" +
		"• 🎉 *Развлечения* — кино, спорт, отдых\n" +
		"• 💊 *Здоровье* — аптеки, врачи\n" +
		"• 🔄 *Регулярные платежи* — аренда, связь, подписки\n" +
		"• 📦 *Прочее* — подарки, непредвиденные траты\n\n" +
		"---\n" +
		"Выбери действие:\n" +
		"• Нажми кнопку ниже, чтобы применить этот набор\n" +
		"• Либо отправь свой список через запятую (например: _Еда, Авто, Дом, Хобби_)"

	return c.Send(categoriesPromptText,
		r.categoriesMenu,
		telebot.ModeMarkdown)
}

func (r *Router) handleUseDefaultCategories(c telebot.Context) error {
	fmt.Println("USER CHOOSED DEFAULT CATEGORIES")
	ctx := context.Background()
	senderId := c.Sender().ID

	defaultCategoriesList := [8]string{
		"Продукты",
		"Кафе и рестораны",
		"Транспорт",
		"Покупки",
		"Развлечения",
		"Здоровье",
		"Регулярные платежи",
		"Прочее",
	}

	// Serializing default categories for saving in the db
	categoriesBytes, err := json.Marshal(defaultCategoriesList)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении профиля. Попробуй позже.")
	}
	if user == nil {
		return c.Send("⚠️ Профиль не найден. Начни с команды /start.")
	}

	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Не удалось сохранить категории. Попробуй еще раз.")
	}

	// 6. Отправляем инструкцию к Шагу 3 (Google Sheets)
	nextStepText := "✅ Стандартные категории успешно подключены!\n\n" +
		"📍 *Шаг 3 из 3: Подключение Google Таблицы*\n\n" +
		"1. Создай копию шаблона таблицы `EM Personal Finances`.\n" +
		"2. Выдай доступ на редактирование сервисному аккаунту бота:\n" +
		fmt.Sprintf("`%s`\n\n", r.cfg.GoogleServiceAccountEmail) + // если есть в конфиге email
		"3. Отправь ссылку на свою готовую копию таблицы в ответном сообщении:"

	return c.Send(nextStepText, telebot.ModeMarkdown)
}
