package telegram

import (
	"context"
	"log/slog"

	"em-finance-bot/config"
	"em-finance-bot/internal/domain"
	db "em-finance-bot/internal/repository/sqlite"
	"em-finance-bot/internal/service/ai"
	"em-finance-bot/internal/service/sheets"

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
	// Register global telemetry & error middleware
	r.bot.Use(r.TelemetryMiddleware())

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
	r.bot.Handle(&telebot.InlineButton{Unique: btnEditTransactionCategory}, r.sendUserCategoriesForEditing)
	r.bot.Handle(&telebot.InlineButton{Unique: btnChangeTransactionCategory}, r.changeTransactionCategory)
	r.bot.Handle(&telebot.InlineButton{Unique: btnBackToEditingTransaction}, r.sendTransactionEditingButtons)
}

func (r *Router) handleIncomingMessage(c telebot.Context) error {
	// Getting request context with trace id
	ctx := c.Get(ContextKey).(context.Context)
	sender := c.Sender()

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

	// State based routing
	switch user.State {
	case domain.StateAwaitingCity:
		return r.handleCityInput(ctx, c, user)
	case domain.StateAwaitingCategories:
		return r.handleUserCustomCategories(ctx, c, user)
	case domain.StateAwaitingSheetURL:
		return r.handleSheetURLInput(ctx, c, user)
	case domain.StateReady:
		return r.handleMoneyOperation(ctx, c, user)
	}

	return nil
}
