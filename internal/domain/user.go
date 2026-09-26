package domain

import (
	"time"
)

type UserState string

const (
	StateNone               UserState = ""
	StateAwaitingCity       UserState = "AWAITING_CITY"
	StateAwaitingCategories UserState = "AWAITING_CATEGORIES"
	StateAwaitingSheetURL   UserState = "AWAITING_SHEET_URL"
	StateReady              UserState = "READY"

	StateAwaitingCategoryClarification UserState = "AWAITING_CATEGORY_CLARIFICATION"
	StateAwaitingEditAmount            UserState = "AWAITING_EDIT_AMOUNT"
	StateAwaitingEditDescription       UserState = "AWAITING_EDIT_DESCRIPTION"
)

type User struct {
	ID                 int
	Username           string
	State              UserState
	Timezone           string
	Currency           string
	CategoriesCache    string
	TelegramID         int64
	SpreadsheetID      string
	LastTransactionID  int    // Last inserted transaction in google sheets
	LastMessageID      int    // Last message sent to user in chat
	PendingTransaction string // Transaction waiting for clarification
	DraftEditTxID      int64  // Transaction ID currently being edited
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
