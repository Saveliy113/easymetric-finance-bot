package telegram

import (
	"em-finance-bot/internal/domain"
	"em-finance-bot/internal/service/sheets"
	"fmt"
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
	// TODO: rename cause one just open categories menu, while another really changes category
	btnEditTransactionCategory    = "edit_transaction_category"
	btnChangeTransactionCategory  = "change_transaction_category"
	btnEditTransactionAmount      = "edit_transaction_amount"
	btnEditTransactionDescription = "edit_transaction_description"
	btnCancelTransactionEditing   = "cancel_transaction_editing"
	btnBackToEditingTransaction   = "back_to_editing_transaction"
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

func basicTransactionMarkup(transaction *sheets.Transaction, user *domain.User) (string, *telebot.ReplyMarkup) {
	var textResponse string
	if transaction.Type == sheets.TypeIncome {
		textResponse = fmt.Sprintf(
			"✅ <b>Доход записан!</b>\n\n"+
				"🆔 ID: <b>#%d</b>\n"+
				"💰 Сумма: <b>%.2f %s</b>\n"+
				"📝 Описание: %s\n"+
				"📅 Дата: %s",
			transaction.ID,
			transaction.Amount,
			user.Currency,
			transaction.Description,
			transaction.Date.Format("02.01.2006 15:04"),
		)
	} else {
		textResponse = fmt.Sprintf(
			"✅ <b>Расход записан!</b>\n\n"+
				"🆔 ID: <b>#%d</b>\n"+
				"💸 Сумма: <b>%.2f %s</b>\n"+
				"📁 Категория: <b>%s</b>\n"+
				"📝 Описание: %s\n"+
				"📅 Дата: %s",
			transaction.ID,
			transaction.Amount,
			user.Currency,
			transaction.Category,
			transaction.Description,
			transaction.Date.Format("02.01.2006 15:04"),
		)
	}

	// Creating buttons
	menu := &telebot.ReplyMarkup{}
	transactionIdStr := strconv.FormatInt(transaction.ID, 10)

	btnEdit := menu.Data("✏️ Изменить", btnQuickEditTransaction, transactionIdStr)
	btnDelete := menu.Data("❌ Удалить", btnQuickDeleteTransaction, transactionIdStr)

	menu.Inline(menu.Row(btnEdit, btnDelete))

	// Returning the text of the receipt and the ready-made layout
	return textResponse, menu
}

func transactionCategoriesMarkup(transactionId int, categories []string) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	var currentRow []telebot.Btn
	for _, category := range categories {
		btn := markup.Data(category, btnChangeTransactionCategory, category)
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 {
			rows = append(rows, markup.Row(currentRow...))
			currentRow = nil
		}
	}

	if len(currentRow) > 0 {
		rows = append(rows, markup.Row(currentRow...))
	}

	btnBack := markup.Data("🔙 Назад", btnBackToEditingTransaction, strconv.Itoa(transactionId))
	rows = append(rows, markup.Row(btnBack))

	markup.Inline(rows...)

	return markup
}
