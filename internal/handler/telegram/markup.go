package telegram

import (
	"strconv"

	"gopkg.in/telebot.v3"
)

const (
	btnIDStartConfig                    = "start_configuration"
	btnIDDefaultCat                     = "use_default_categories"
	btnIDCustomCat                      = "custom_categories"
	btnIDSelectClarifiedCategory        = "select_clarified_category"
	btnIDCancelTransactionClarification = "cancel_transaction_clarification"
	btnQuickEditTransaction             = "quick_edit_transaction"
	btnQuickDeleteTransaction           = "quick_delete_transaction"
	btnEditTransactionCategory          = "edit_transaction_category"
	btnEditTransactionAmount            = "edit_transaction_amount"
	btnEditTransactionDescription       = "edit_transaction_description"
	btnCancelTransactionEditing         = "cancel_transaction_editing"
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

func transactionEditingButtonsMarkup(transactionId int) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	transactionIdStr := strconv.Itoa(transactionId)

	btnEdit := markup.Data("🏷 Сменить категорию", btnEditTransactionCategory, transactionIdStr)
	btnEditAmount := markup.Data("💰 Изменить сумму", btnEditTransactionAmount, transactionIdStr)
	btnEditDescription := markup.Data("✏️ Изменить описание", btnEditTransactionDescription, transactionIdStr)
	btnCancel := markup.Data("🔙 Отменить", btnCancelTransactionEditing, transactionIdStr)

	// 2x2 grid
	markup.Inline(
		markup.Row(btnEdit, btnEditAmount),
		markup.Row(btnEditDescription, btnCancel),
	)

	return markup
}
