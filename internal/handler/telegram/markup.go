package telegram

import "gopkg.in/telebot.v3"

const (
	btnIDStartConfig                    = "start_configuration"
	btnIDDefaultCat                     = "use_default_categories"
	btnIDCustomCat                      = "custom_categories"
	btnIDSelectClarifiedCategory        = "select_clarified_category"
	btnIDCancelTransactionClarification = "cancel_transaction_clarification"
	btnQuickEditTransaction             = "quick_edit_transaction"
	btnQuickDeleteTransaction           = "quick_delete_transaction"
	
)

func (r *Router) startConfigMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btn := markup.Data("⚙️ Начать настройку", btnIDStartConfig)
	markup.Inline(markup.Row(btn))

	return markup
}

func (r *Router) categoriesMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnDefault := markup.Data("✅ Использовать стандартные", btnIDDefaultCat)
	markup.Inline(markup.Row(btnDefault))

	return markup
}

func (r *Router) defaultCategoriesMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnDefault := markup.Data("✅ Использовать стандартные", btnIDDefaultCat)

	markup.Inline(markup.Row(btnDefault))

	return markup
}
