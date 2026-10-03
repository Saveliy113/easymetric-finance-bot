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
	btnOpenTransactionCategoriesMenu    = "open_tx_categories_menu"
	btnChangeTransactionCategory        = "change_transaction_category"
	btnEditTransactionAmount      = "edit_transaction_amount"
	btnEditTransactionDescription = "edit_transaction_description"
	btnCancelTransactionEditing   = "cancel_transaction_editing"
	btnBackToEditingTransaction   = "back_to_editing_transaction"
	btnEditCategories             = "edit_categories"
	btnCategoriesAdd              = "categories_add"
	btnCategoriesDelete           = "categories_delete"
	btnDeleteCategoryItem         = "delete_category_item"
	btnBackToCategories           = "back_to_categories"
	btnChangeCity                 = "change_city"
	btnLinkNewTable               = "link_new_table"
	btnCancelSettings             = "cancel_settings"
)

type MenuUI struct {
	ReplyMenu *telebot.ReplyMarkup

	// Menu buttons
	BtnTable    telebot.Btn
	BtnSummary  telebot.Btn
	BtnSettings telebot.Btn
	BtnHelp     telebot.Btn
}

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

func transactionEditingButtonsMarkup(transactionID int) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	transactionIDStr := strconv.Itoa(transactionID)

	btnEdit := markup.Data("🏷 Сменить категорию", btnOpenTransactionCategoriesMenu, transactionIDStr)
	btnEditAmount := markup.Data("💰 Изменить сумму", btnEditTransactionAmount, transactionIDStr)
	btnEditDescription := markup.Data("✏️ Изменить описание", btnEditTransactionDescription, transactionIDStr)
	btnCancel := markup.Data("🔙 Отменить", btnCancelTransactionEditing, transactionIDStr)

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
	transactionIDStr := strconv.FormatInt(transaction.ID, 10)

	btnEdit := menu.Data("✏️ Изменить", btnQuickEditTransaction, transactionIDStr)
	btnDelete := menu.Data("❌ Удалить", btnQuickDeleteTransaction, transactionIDStr)

	menu.Inline(menu.Row(btnEdit, btnDelete))

	// Returning the text of the receipt and the ready-made layout
	return textResponse, menu
}

func deletedTransactionText(transaction *sheets.Transaction, user *domain.User) string {
	dateStr := ""
	if !transaction.Date.IsZero() {
		dateStr = transaction.Date.Format("02.01.2006 15:04")
	}

	if transaction.Type == sheets.TypeIncome {
		return fmt.Sprintf(
			"🗑 <b>Доход #%d удален!</b>\n\n"+
				"• <b>Сумма:</b> <s>%.2f %s</s>\n"+
				"• <b>Описание:</b> <s>%s</s>\n"+
				"• <b>Дата:</b> <code>%s</code>",
			transaction.ID,
			transaction.Amount,
			user.Currency,
			transaction.Description,
			dateStr,
		)
	}

	return fmt.Sprintf(
		"🗑 <b>Расход #%d удален!</b>\n\n"+
			"• <b>Сумма:</b> <s>%.2f %s</s>\n"+
			"• <b>Категория:</b> <s>%s</s>\n"+
			"• <b>Описание:</b> <s>%s</s>\n"+
			"• <b>Дата:</b> <code>%s</code>",
		transaction.ID,
		transaction.Amount,
		user.Currency,
		transaction.Category,
		transaction.Description,
		dateStr,
	)
}

func transactionCategoriesMarkup(transactionID int, categories []string) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	var currentRow []telebot.Btn
	for idx, category := range categories {
		payload := fmt.Sprintf("%d|%d", transactionID, idx)
		btn := markup.Data(category, btnChangeTransactionCategory, payload)
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 {
			rows = append(rows, markup.Row(currentRow...))
			currentRow = nil
		}
	}

	if len(currentRow) > 0 {
		rows = append(rows, markup.Row(currentRow...))
	}

	btnBack := markup.Data("🔙 Назад", btnBackToEditingTransaction, strconv.Itoa(transactionID))
	rows = append(rows, markup.Row(btnBack))

	markup.Inline(rows...)

	return markup
}

func cancelTransactionEditingMarkup(transactionID int64) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnCancel := markup.Data("🔙 Отмена", btnCancelTransactionEditing, strconv.FormatInt(transactionID, 10))
	markup.Inline(markup.Row(btnCancel))

	return markup
}

func NewMenuUI() *MenuUI {
	markup := &telebot.ReplyMarkup{
		RemoveKeyboard: true,
	}

	btnTable := markup.Text("📊 Таблица")
	btnSummary := markup.Text("📈 Отчет")
	btnSettings := markup.Text("⚙️ Настройки")
	btnHelp := markup.Text("❓ Помощь")

	return &MenuUI{
		ReplyMenu:   markup,
		BtnTable:    btnTable,
		BtnSummary:  btnSummary,
		BtnSettings: btnSettings,
		BtnHelp:     btnHelp,
	}
}

// SystemCommands returns the list of bot commands displayed in Telegram native Menu button
func SystemCommands() []telebot.Command {
	return []telebot.Command{
		{Text: "report", Description: "📈 Отчет за период"},
		{Text: "table", Description: "📊 Google Таблица"},
		{Text: "settings", Description: "⚙️ Настройки"},
		{Text: "help", Description: "❓ Помощь"},
	}
}

func MenuSettingsMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}

	btnCategories := markup.Data("🏷 Категории", btnEditCategories)
	btnLocation := markup.Data("🌍 Локация", btnChangeCity)
	btnTable := markup.Data("🔗 Сменить таблицу", btnLinkNewTable)

	// Settings menu grid
	markup.Inline(
		markup.Row(btnCategories, btnLocation, btnTable),
	)

	return markup
}

func MenuHelpMarkup(sheetID string) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}

	// Generating table link
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", sheetID)

	// Inline button with integrated URL
	btnOpenSheet := markup.URL("Открыть Google Sheets", sheetURL)
	markup.Inline(markup.Row(btnOpenSheet))

	return markup
}

func cancelSettingsMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnCancel := markup.Data("🔙 Отмена", btnCancelSettings)
	markup.Inline(markup.Row(btnCancel))

	return markup
}

func categoriesManagementMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnAdd := markup.Data("➕ Добавить", btnCategoriesAdd)
	btnDelete := markup.Data("🗑 Удалить", btnCategoriesDelete)
	btnBack := markup.Data("🔙 Назад", btnCancelSettings)

	markup.Inline(
		markup.Row(btnAdd, btnDelete),
		markup.Row(btnBack),
	)

	return markup
}

func cancelAddCategoryMarkup() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	btnBack := markup.Data("🔙 Отмена", btnBackToCategories)
	markup.Inline(markup.Row(btnBack))

	return markup
}

func categoriesDeleteMarkup(categories []string) *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	var currentRow []telebot.Btn
	for idx, cat := range categories {
		btn := markup.Data("❌ "+cat, btnDeleteCategoryItem, strconv.Itoa(idx))
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 {
			rows = append(rows, markup.Row(currentRow...))
			currentRow = nil
		}
	}

	if len(currentRow) > 0 {
		rows = append(rows, markup.Row(currentRow...))
	}

	btnBack := markup.Data("🔙 Назад", btnBackToCategories)
	rows = append(rows, markup.Row(btnBack))

	markup.Inline(rows...)

	return markup
}


