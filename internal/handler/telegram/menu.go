package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"em-finance-bot/internal/domain"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMainMenuHelp(c telebot.Context) error {
	helpMessage := "📖 <b>Как устроен и работает бот EM Personal Finances</b>\n\n" +
		"Бот помогает вести учет личных финансов: вы присылаете сообщения о расходах и доходах, искусственный интеллект разбирает их и сразу добавляет запись в вашу Google Таблицу.\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━\n" +
		"💬 <b>1. Запись расходов и доходов</b>\n" +
		"Операции можно отправлять обычным текстом или <b>голосовыми сообщениями</b>:\n" +
		"• <code>Кофе 350</code> — запишет расход в нужную категорию на сегодня.\n" +
		"• <code>Обед 850 вчера</code> — сохранит трату со вчерашней датой.\n" +
		"• <code>Такси 450</code> — определит категорию «Транспорт».\n" +
		"• <code>Зарплата 150000</code> — запишет как «Доход».\n" +
		"• <code>Продукты 2300 в супермаркете</code> — сохранит сумму и комментарий к покупке.\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━\n" +
		"🏷 <b>2. Определение категории</b>\n" +
		"• Если назначение траты понятно сразу, бот вносит запись без лишних вопросов.\n" +
		"• Если назначить категорию однозначно не получилось, бот покажет кнопки с подходящими вариантами или кнопкой «Отменить запись».\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━\n" +
		"🌍 <b>3. Город и часовой пояс</b>\n" +
		"Время операций и слова «вчера», «сегодня» считаются по вашему часовому поясу.\n" +
		"• Изменить город и время можно через: <b>⚙️ Настройки</b> ➔ <b>Локация / Часовой пояс</b>.\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━\n" +
		"📊 <b>4. Ваша Google Таблица</b>\n" +
		"• <b>Лист «Дашборд»:</b> здесь хранится список всех операций и верхняя панель с итогами месяца (общий доход, расход и чистый баланс).\n" +
		"• <b>Выбор периода:</b> в ячейках <b>B3 (Год)</b> и <b>B4 (Месяц)</b> можно переключать дату, чтобы посмотреть статистику за прошлые месяцы.\n" +
		"• <b>Лист «Аналитика»:</b> наглядная диаграмма, которая показывает, на что уходит больше всего денег.\n\n" +
		"━━━━━━━━━━━━━━━━━━━━━\n" +
		"⌨️ <b>5. Кнопки меню</b>\n" +
		"• <b>📊 Таблица</b> — быстрая ссылка на вашу Google Таблицу.\n" +
		"• <b>📈 Итоги месяца</b> — сколько заработано, потрачено и осталось в этом месяце.\n" +
		"• <b>🏷 Категории</b> — список категорий для ваших расходов.\n" +
		"• <b>⚙️ Настройки</b> — смена города, часового пояса, валюты или подключение другой таблицы.\n" +
		"• <b>❓ Помощь</b> — вызов этой инструкции со всеми правилами работы и командами."

	return c.Send(helpMessage, telebot.ModeHTML)
}

func (r *Router) handleMainMenuTable(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Extracting userID
	userID := c.Sender().ID

	// Searching user in the db
	user, err := r.userRepo.GetByTelegramId(ctx, userID)
	if err != nil {
		return err
	}

	// Generating table link markup
	markup := MenuHelpMarkup(user.SpreadsheetID)

	msg := "📊 <b>Ваша персональная финансовая таблица</b>\n\n" +
		"Нажмите на кнопку ниже, чтобы перейти к таблице:"

	return c.Send(msg, markup, telebot.ModeHTML)
}

func (r *Router) handleMainMenuSettings(c telebot.Context) error {
	// Generating settings markup
	markup := MenuSettingsMarkup()

	msg := "⚙️ <b>Настройки</b>\n\n" +
		"Выберите раздел, который хотите настроить"

	return c.Send(msg, markup, telebot.ModeHTML)
}

func (r *Router) handleChangeCity(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to telegram to stop loading
	if c.Callback() != nil {
		_ = c.Respond()
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Updating user state to await city
	user.State = domain.StateAwaitingCity
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	msg := "🌍 Отправьте название вашего города"

	if c.Callback() != nil {
		return c.Edit(msg, cancelSettingsMarkup(), telebot.ModeHTML)
	}

	return c.Send(msg, cancelSettingsMarkup(), telebot.ModeHTML)
}

func (r *Router) handleLinkNewTable(c telebot.Context) error {
	ctx := c.Get(ContextKey).(context.Context)

	// Responding to telegram to stop loading
	if c.Callback() != nil {
		_ = c.Respond()
	}

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Updating user state to await new table link
	user.State = domain.StateAwaitingNewTable
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	msg := "📊 Отправьте новую ссылку на таблицу"

	if c.Callback() != nil {
		return c.Edit(msg, cancelSettingsMarkup(), telebot.ModeHTML)
	}

	return c.Send(msg, cancelSettingsMarkup(), telebot.ModeHTML)
}

func (r *Router) handleCancelSettings(c telebot.Context) error {
	_ = c.Respond()
	ctx := c.Get(ContextKey).(context.Context)

	// Getting user from the db
	user, err := r.userRepo.GetByTelegramId(ctx, c.Sender().ID)
	if err != nil {
		return err
	}

	// Resetting state back to ready
	user.State = domain.StateReady
	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	// Returning back to settings menu
	markup := MenuSettingsMarkup()
	msg := "⚙️ <b>Настройки</b>\n\n" +
		"Выберите раздел, который хотите настроить"

	return c.Edit(msg, markup, telebot.ModeHTML)
}

func (r *Router) handleChangeTableURLInput(ctx context.Context, c telebot.Context, user *domain.User) error {
	sheetUrl := strings.TrimSpace(c.Text())
	slog.InfoContext(ctx, "Получена новая ссылка на Google Таблицу:",
		slog.Int64("user_id", user.TelegramID),
		slog.String("url", sheetUrl),
	)

	// Extracting unique sheet id from url
	slog.InfoContext(ctx, "Извлекаем уникальный id таблицы из ссылки")
	sheetIDRegex := regexp.MustCompile(`/d/([a-zA-Z0-9_-]+)`)
	matches := sheetIDRegex.FindStringSubmatch(sheetUrl)
	if len(matches) < 2 {
		return domain.ErrInvalidSheetURL
	}

	sheetID := matches[1]

	waitMsg, _ := r.bot.Send(c.Chat(), "⏳ Проверяю доступ к таблице...")

	// Checking the bot is able to operate with the table
	slog.InfoContext(ctx, "Проверяем доступ к Google Таблице",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	err := r.sheetsService.ValidateAccess(ctx, sheetID)
	if waitMsg != nil {
		_ = r.bot.Delete(waitMsg)
	}

	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Доступ к Google Таблице успешно подтвержден",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	// Setup user categories in the connected sheet if available
	if user.CategoriesCache != "" {
		var categories []string
		if err := json.Unmarshal([]byte(user.CategoriesCache), &categories); err == nil && len(categories) > 0 {
			if err := r.sheetsService.SetupUserCategories(ctx, sheetID, "Дашборд", categories); err != nil {
				return err
			}
		}
	}

	// Saving new sheet id to the db
	slog.InfoContext(ctx, "Сохраняем id таблицы в БД",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	user.SpreadsheetID = sheetID
	user.State = domain.StateReady

	if err := r.userRepo.Upsert(ctx, user); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Id таблицы успешно сохранен в БД",
		slog.Int64("user_id", user.TelegramID),
		slog.String("sheet_id", sheetID),
	)

	successMessage := fmt.Sprintf(
		"✅ <b>Новая Google Таблица успешно подключена!</b>\n\n"+
			"🔗 <a href=\"%s\">Открыть Google Таблицу</a>\n\n"+
			"Все новые операции будут автоматически записываться в эту таблицу.",
		sheetUrl,
	)

	return c.Send(successMessage, telebot.ModeHTML, r.menuUI.ReplyMenu)
}



