package telegram

import (
	"context"

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
	// Start comand handler (/start)
	r.bot.Handle("/start", r.handleGreeting)

	// Configuration step handler
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDStartConfig}, r.handleStartConfiguration)

	// Default categories button handler
	r.bot.Handle(&telebot.InlineButton{Unique: btnIDDefaultCat}, r.handleUseDefaultCategories)

	// Text and voice messages handler
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
