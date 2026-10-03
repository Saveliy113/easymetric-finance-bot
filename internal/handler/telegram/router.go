package telegram

import (
	"context"
	"log/slog"
	"time"

	"em-finance-bot/config"
	"em-finance-bot/internal/domain"
	db "em-finance-bot/internal/repository/sqlite"
	"em-finance-bot/internal/service/ai"
	"em-finance-bot/internal/service/sheets"

	"gopkg.in/telebot.v3"
)

type Router struct {
	bot           *telebot.Bot
	cfg           *config.Config
	userRepo      *db.UserRepository
	aiService     *ai.GeminiService
	sheetsService *sheets.SheetsService
	menuUI        *MenuUI
}

func NewRouter(bot *telebot.Bot, cfg *config.Config, userRepo *db.UserRepository, aiService *ai.GeminiService, sheetsService *sheets.SheetsService) *Router {
	return &Router{
		bot:           bot,
		cfg:           cfg,
		userRepo:      userRepo,
		aiService:     aiService,
		sheetsService: sheetsService,
		menuUI:        NewMenuUI(),
	}
}

// Telegram comands and events registration
func (r *Router) Register() {
	// Register global telemetry & error middleware
	r.bot.Use(r.TelemetryMiddleware())
	r.bot.Use(r.RateLimitMiddleware())

	// Start comand handler (/start)
	r.bot.Handle("/start", r.handleGreeting)

	// Configuration step handler
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDStartConfig}, r.handleStartConfiguration)

	// Default categories button handler
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDDefaultCat}, r.handleUseDefaultCategories)

	// Text and voice messages handler
	r.bot.Handle(telebot.OnText, r.handleIncomingMessage)
	r.bot.Handle(telebot.OnVoice, r.handleIncomingMessage)

	// Transaction category clarification handler
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDSelectClarifiedCategory}, r.handleSelectClarifiedCategory)
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDCancelTransactionClarification}, r.handleCancelTransactionClarification)

	// Transaction quick edit and delete handlers
	r.bot.Handle(&telebot.InlineButton{Unique: btnQuickEditTransaction}, r.sendTransactionEditingButtons)
	r.bot.Handle(&telebot.InlineButton{Unique: btnQuickDeleteTransaction}, r.handleDeleteTransaction)
	r.bot.Handle(&telebot.InlineButton{Unique: btnCancelTransactionEditing}, r.handleCancelTransactionEditing)
	r.bot.Handle(&telebot.InlineButton{Unique: btnOpenTransactionCategoriesMenu}, r.sendUserCategoriesForEditing)
	r.bot.Handle(&telebot.InlineButton{Unique: btnChangeTransactionCategory}, r.changeTransactionCategory)
	r.bot.Handle(&telebot.InlineButton{Unique: btnEditTransactionDescription}, r.sendTransactionDescriptionForEditing)
	r.bot.Handle(&telebot.InlineButton{Unique: btnEditTransactionAmount}, r.sendTransactionAmountForEditing)
	r.bot.Handle(&telebot.InlineButton{Unique: btnBackToEditingTransaction}, r.sendTransactionEditingButtons)

	// Register Telegram system menu commands (shows native [Menu] button on mobile)
	if err := r.bot.SetCommands(SystemCommands()); err != nil {
		slog.Warn("Не удалось установить команды меню Telegram", slog.Any("error", err))
	}

	// Slash commands handlers
	r.bot.Handle("/report", r.handleMainMenuSummary)
	r.bot.Handle("/summary", r.handleMainMenuSummary)
	r.bot.Handle("/table", r.handleMainMenuTable)
	r.bot.Handle("/settings", r.handleMainMenuSettings)
	r.bot.Handle("/help", r.handleMainMenuHelp)

	// Main menu handlers (legacy reply buttons support)
	r.bot.Handle(&r.menuUI.BtnHelp, r.handleMainMenuHelp)
	r.bot.Handle(&r.menuUI.BtnTable, r.handleMainMenuTable)
	r.bot.Handle(&r.menuUI.BtnSettings, r.handleMainMenuSettings)
	r.bot.Handle(&r.menuUI.BtnSummary, r.handleMainMenuSummary)

	// Settings menu handlers
	r.bot.Handle(&telebot.InlineButton{Unique: btnEditCategories}, r.handleSettingsCategoriesMenu)
	r.bot.Handle(&telebot.InlineButton{Unique: btnCategoriesAdd}, r.handleCategoriesAddClick)
	r.bot.Handle(&telebot.InlineButton{Unique: btnCategoriesDelete}, r.handleCategoriesDeleteMenu)
	r.bot.Handle(&telebot.InlineButton{Unique: btnDeleteCategoryItem}, r.handleDeleteCategoryClick)
	r.bot.Handle(&telebot.InlineButton{Unique: btnBackToCategories}, r.handleSettingsCategoriesMenu)
	r.bot.Handle(&telebot.InlineButton{Unique: btnChangeCity}, r.handleChangeCity)
	r.bot.Handle(&telebot.InlineButton{Unique: btnLinkNewTable}, r.handleLinkNewTable)
	r.bot.Handle(&telebot.InlineButton{Unique: btnCancelSettings}, r.handleCancelSettings)
}

func (r *Router) handleIncomingMessage(c telebot.Context) error {
	// Getting request context with trace id
	ctx := c.Get(ContextKey).(context.Context)
	sender := c.Sender()

	// Guard: only work in private chats
	if c.Chat().Type != telebot.ChatPrivate {
		return nil
	}

	slog.InfoContext(ctx, "💬 Входящее сообщение",
		slog.Int64("user_id", sender.ID),
	)

	// Getting user from the db
	slog.InfoContext(ctx, "Ищем пользователя в базе данных...",
		slog.Int64("user_id", sender.ID),
	)

	user, err := r.userRepo.GetByTelegramId(ctx, sender.ID)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Пользователь найден",
		slog.Int64("user_id", sender.ID),
		slog.String("state", string(user.State)),
	)

	// Auto-reset stale states (user abandoned mid-flow > 30 minutes ago)
	const stateTimeout = 30 * time.Minute
	if user.State != domain.StateReady && user.State != domain.StateNone {
		if time.Since(user.UpdatedAt) > stateTimeout {
			slog.InfoContext(ctx, "Автоматический сброс устаревшего состояния пользователя",
				slog.Int64("user_id", sender.ID),
				slog.String("stale_state", string(user.State)),
				slog.Duration("elapsed", time.Since(user.UpdatedAt)),
			)
			user.State = domain.StateReady
			user.DraftEditTxID = 0
			_ = r.userRepo.Upsert(ctx, user)
			// Fall through to StateReady handler below
		}
	}

	// State based routing
	switch user.State {
	case domain.StateAwaitingCity:
		return r.handleCityInput(ctx, c, user)
	case domain.StateAwaitingCategories:
		return r.handleUserCustomCategories(ctx, c, user)
	case domain.StateAwaitingSheetURL:
		return r.handleSheetURLInput(ctx, c, user)
	case domain.StateAwaitingNewTable:
		return r.handleChangeTableURLInput(ctx, c, user)
	case domain.StateAwaitingAnalyticsDates:
		return r.handleAnalyticsDatesInput(ctx, c, user)
	case domain.StateAwaitingEditAmount:
		return r.handleEditTransactionAmount(ctx, c, user)
	case domain.StateAwaitingEditDescription:
		return r.handleEditTransactionDescription(ctx, c, user)
	case domain.StateAwaitingCategoryClarification:
		// User sent text instead of clicking inline button — remind them
		return c.Send("👆 Пожалуйста, выберите категорию, нажав на одну из кнопок выше, или отмените запись.")
	case domain.StateReady:
		return r.handleMoneyOperation(ctx, c, user)
	case domain.StateNone:
		// User exists but never completed onboarding
		return c.Send("👋 Похоже, ты еще не завершил настройку. Отправь /start для начала работы.")
	}

	return nil
}
