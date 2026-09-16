package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"em-finance-bot/config"
	"em-finance-bot/internal/domain"
	db "em-finance-bot/internal/repository/sqlite"
	"em-finance-bot/internal/service/ai"
	"em-finance-bot/internal/service/sheets"
	"em-finance-bot/pkg/idgen"
	"em-finance-bot/pkg/trace"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot            *telebot.Bot
	cfg            *config.Config
	userRepo       *db.UserRepository
	aiService      *ai.GeminiService
	sheetsService  *sheets.SheetsService
	categoriesMenu *telebot.ReplyMarkup
}

func NewRouter(bot *telebot.Bot, cfg *config.Config, userRepo *db.UserRepository, aiService *ai.GeminiService, sheetsService *sheets.SheetsService) *Router {
	return &Router{
		bot:           bot,
		cfg:           cfg,
		userRepo:      userRepo,
		aiService:     aiService,
		sheetsService: sheetsService,
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
	r.bot.Handle("/start", r.handleGreeting)

	r.bot.Handle(telebot.OnText, r.handleIncomingMessage)
	r.bot.Handle(telebot.OnVoice, r.handleIncomingMessage)
}

func (r *Router) handleIncomingMessage(c telebot.Context) error {
	// Creating request trace id and creating context with it
	traceId := idgen.Short()
	ctx := trace.WithId(context.Background(), traceId)
	sender := c.Sender()

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, sender.ID)
	if err != nil {
		return r.handleError(ctx, c, err)
	}

	// State based routing
	switch user.State {
	case domain.StateAwaitingCity:
		return r.handleCityInput(c, user)
	case domain.StateAwaitingCategories:
		return r.handleUserCustomCategories(c)
	case domain.StateAwaitingSheetURL:
		return r.handleSheetURLInput(c)
	case domain.StateReady:
		return r.handleMoneyOperation(c)
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

func (r *Router) handleUserCustomCategories(c telebot.Context) error {
	ctx := context.Background()
	senderId := c.Sender().ID
	inputCategories := strings.TrimSpace(c.Text())

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Анализирую категории трат...")

	// Getting data using gemini
	categoriesInfo, err := r.aiService.ParseCategories(ctx, inputCategories)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		fmt.Printf("Error while detecting categories: %v", err)
		return c.Send("⚠️ Ошибка при анализе категорий. Попробуй еще раз.")

	}

	if !categoriesInfo.IsValid && categoriesInfo.ErrorMessage != "" {
		return c.Send(categoriesInfo.ErrorMessage)
	}

	fmt.Println("USER CHOOSED CUSTOM CATEGORIES:", categoriesInfo.Categories)

	// Saving user's categories to db
	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении профиля. Попробуй позже.")
	}
	if user == nil {
		return c.Send("⚠️ Профиль не найден. Начни с команды /start.")
	}

	// Serializing default categories for saving in the db
	categoriesBytes, err := json.Marshal(categoriesInfo.Categories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	// Setup user categories in sheets
	if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", categoriesInfo.Categories); err != nil {
		slog.Error("Не удалось настроить категории", "ошибка", err)
	}

	// Saving user to the db
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Не удалось сохранить категории. Попробуй еще раз.")
	}

	// Sending instructions for step 3 - connecting Google Sheets
	nextStepText := fmt.Sprintf(
		"✅ Категории успешно подключены!\n\n"+
			"📍 *Шаг 3 из 3: Подключение Google Таблицы*\n\n"+
			"1. [Создай копию шаблона таблицы EM Personal Finances](%s)\n"+
			"2. Выдай доступ на редактирование сервисному аккаунту бота:\n`%s`\n\n"+
			"3. Отправь ссылку на свою готовую копию таблицы в ответном сообщении:",
		r.cfg.TemplateSheetURL,
		r.cfg.GoogleServiceAccountEmail,
	)

	return c.Send(nextStepText, telebot.ModeMarkdown)
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

	// Setup user categories in sheets
	if err := r.sheetsService.SetupUserCategories(ctx, user.SpreadsheetID, "Дашборд", defaultCategoriesList[:]); err != nil {
		slog.Error("Не удалось настроить категории", "ошибка", err)
	}

	// Saving user to the db
	user.CategoriesCache = string(categoriesBytes)
	user.State = domain.StateAwaitingSheetURL

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return c.Send("⚠️ Не удалось сохранить категории. Попробуй еще раз.")
	}

	// 6. Отправляем инструкцию к Шагу 3 (Google Sheets)
	nextStepText := fmt.Sprintf(
		"✅ Категории успешно подключены!\n\n"+
			"📍 *Шаг 3 из 3: Подключение Google Таблицы*\n\n"+
			"1. [Создай копию шаблона таблицы EM Personal Finances](%s)\n"+
			"2. Выдай доступ на редактирование сервисному аккаунту бота:\n`%s`\n\n"+
			"3. Отправь ссылку на свою готовую копию таблицы в ответном сообщении:",
		r.cfg.TemplateSheetURL,
		r.cfg.GoogleServiceAccountEmail,
	)

	return c.Send(nextStepText, telebot.ModeMarkdown)
}

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

func (r *Router) handleMoneyOperation(c telebot.Context) error {
	ctx := context.Background()
	senderId := c.Sender().ID

	// Detect the message type: text or speach and get message text
	var inputText string

	if c.Message().Voice == nil {
		inputText = strings.TrimSpace(c.Text())
	} else {
		waitVoiceMsg, _ := r.bot.Send(c.Chat(), "🎙 Слушаю голосовое...")

		// Downloading audio file from tg
		voiceFile, err := r.bot.File(&c.Message().Voice.File)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}

			return c.Send("⚠️ Не удалось загрузить голосовое сообщение. Попробуй ещё раз.")
		}

		defer voiceFile.Close()

		voiceBytes, err := io.ReadAll(voiceFile)
		if err != nil {
			if waitVoiceMsg != nil {
				_ = r.bot.Delete(waitVoiceMsg)
			}
			return c.Send("⚠️ Ошибка при чтении аудиофайла.")
		}

		// Getting transcription with Gemini
		transcription, err := r.aiService.TranscribeVoice(ctx, voiceBytes)
		if waitVoiceMsg != nil {
			_ = r.bot.Delete(waitVoiceMsg)
		}

		if err != nil || strings.TrimSpace(transcription) == "" {
			return c.Send("⚠️ Не удалось разобрать слова в голосовом. Попробуй написать текстом.")
		}

		inputText = strings.TrimSpace(transcription)
	}

	if inputText == "" {
		return c.Send("⚠️ Не удалось распознать сообщение. Попробуй ещё раз.")
	}

	// Getting user data from the db
	user, err := r.userRepo.GetByTelegramId(ctx, senderId)
	if err != nil {
		return c.Send("⚠️ Ошибка при получении данных пользователя")
	}

	// Deserializing user expenses categories
	var categories []string
	if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err != nil {
		return c.Send("⚠️ Ошибка при обработке данных по категориям пользователя")
	}

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Обрабатываю операцию...")

	// Pass all information to ai service
	transaction, err := r.aiService.ParsedTransaction(ctx, inputText, categories, user.Currency, user.Timezone)
	if err != nil || !transaction.IsValid {
		fmt.Println("ERROR PARSING TRANSACTION", err)
		return c.Send("⚠️ Не удалось распознать операцию или сумму.\nПример: `Такси 1200` или `Зарплата 350000`", telebot.ModeMarkdown)
	}

	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	// Handling transaction clarification
	// if transaction.NeedsClarification && len(transaction.SuggestedCategories) > 0 {
	// 	// Сохраняем черновик транзакции во временное поле пользователя
	// 	txBytes, _ := json.Marshal(transaction)
	// 	user.PendingTransaction = string(txBytes)
	// 	user.State = domain.StateAwaitingCategoryClarification
	// 	_ = r.userRepo.Upsert(ctx, user)

	// 	// Формируем кнопки с предложенными вариантами
	// 	clarifyMarkup := &telebot.ReplyMarkup{}
	// 	var rows []telebot.Row
	// 	for _, cat := range transaction.SuggestedCategories {
	// 		btn := clarifyMarkup.Data(cat, "set_cat", cat)
	// 		rows = append(rows, clarifyMarkup.Row(btn))
	// 	}
	// 	clarifyMarkup.Inline(rows...)

	// 	prompt := fmt.Sprintf(
	// 		"🤔 Нашел операцию: *%s* на сумму `%.2f %s`, но сомневаюсь в категории.\n\nВыбери подходящую категорию:",
	// 		transaction.Description,
	// 		transaction.Amount,
	// 		user.Currency,
	// 	)
	// 	return c.Send(prompt, clarifyMarkup, telebot.ModeMarkdown)
	// }

	// Saving operation to Google sheets
	parsedDate, err := ai.ParseTransactionDate(transaction.Date, user.Timezone)
	if err != nil {
		slog.Error("Не удалось разобрать дату транзакции", "дата", transaction.Date, "таймзона", user.Timezone, "ошибка", err)
		return c.Send("⚠️ Не удалось распознать дату операции. Попробуй ещё раз.")
	}

	if err = r.sheetsService.SaveTransaction(ctx, user.SpreadsheetID, &sheets.Transaction{
		UserID:      user.TelegramID,
		Type:        sheets.TransactionType(transaction.Type), // "expense" или "income"
		Amount:      transaction.Amount,
		Category:    transaction.Category,
		Description: transaction.Description,
		Date:        parsedDate,
		CreatedAt:   time.Now(),
	}); err != nil {
		slog.Error("Не удалось сохранить операцию в Google Таблицу", "ошибка", err, "user_id", user.TelegramID)
		return c.Send("⚠️ Произошла ошибка при записи в таблицу. Попробуй позже.")
	}

	var textResponse string
	if transaction.Type == string(sheets.TypeIncome) { //TODO: fix type
		textResponse = fmt.Sprintf("✅ <b>Доход записан!</b>\n\n💰 Сумма: <b>%.2f</b>\n📝 Описание: %s\n📅 Дата: %s",
			transaction.Amount,
			transaction.Description,
			parsedDate.Format("02.01.2006"),
		)
	} else {
		textResponse = fmt.Sprintf("✅ <b>Расход записан!</b>\n\n💸 Сумма: <b>%.2f</b>\n📁 Категория: <b>%s</b>\n📝 Описание: %s\n📅 Дата: %s",
			transaction.Amount,
			transaction.Category,
			transaction.Description,
			parsedDate.Format("02.01.2006"),
		)
	}

	return c.Send(textResponse, telebot.ModeHTML)
}

func (r *Router) handleError(ctx context.Context, c telebot.Context, err error) error {
	// Ignoring request cancelation by user or due to timeout
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		slog.DebugContext(ctx, "Запрос отменен клиентом или прерван по таймауту", slog.Any("error", err))
		return nil
	}

	// User metadata for logging (if available)
	var userID int64
	var username string
	if sender := c.Sender(); sender != nil {
		userID = sender.ID
		username = sender.Username
	}

	// Handling business errors (Known Domain/Service Errors)
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		slog.WarnContext(ctx, "Пользователь не найден в системе",
			slog.Int64("user_id", userID),
			slog.String("username", username),
		)

		return c.Send("Похоже, что ты еще не зарегистрирован. Отправь /start для начала работы.")
	}

	// Handling technical errors
	traceID := trace.FromContext(ctx)

	slog.ErrorContext(ctx, "Внутренний системный сбой",
		slog.Int64("user_id", userID),
		slog.String("username", username),
		slog.Any("error", err),
	)

	userMsg := fmt.Sprintf(
		"⚠️ <b>Произошла внутренняя ошибка.</b>\n\n"+
			"Попробуй ещё раз позже. Если проблема не решится, перешли это разработчику <a href=\"https://t.me/saveliy_d13\">@saveliy_d13</a>:\n\n"+
			"<code>Код ошибки: %s</code>",
		traceID,
	)

	return c.Send(userMsg, telebot.ModeHTML)
}
