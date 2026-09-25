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
)

type User struct {
	ID              int
	TelegramID      int64
	SpreadsheetID   string
	LastTransactionID int
	PendingTransaction string
	Username        string
	State           UserState
	Timezone        string
	Currency        string
	CategoriesCache string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
