package domain

import (
	"time"
)

type TransactionType string

const (
	TypeExpense TransactionType = "expense"
	TypeIncome  TransactionType = "income"
)

type Transaction struct {
	ID          int64           `json:"id" db:"id"`
	UserID      int64           `json:"user_id" db:"user_id"`
	Type        TransactionType `json:"type" db:"type"`
	Amount      float64         `json:"amount" db:"amount"`
	Category    string          `json:"category" db:"category"`
	Description string          `json:"description" db:"description"`
	Date        time.Time       `json:"date" db:"date"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}
