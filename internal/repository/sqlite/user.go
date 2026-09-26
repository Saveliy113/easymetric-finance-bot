package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"em-finance-bot/internal/domain"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByTelegramId(ctx context.Context, telegramId int64) (*domain.User, error) {
	query := `
		SELECT id,
			telegram_id as telegramId,
			spreadsheet_id as spreadsheetId,
			last_transaction_id as lastTransactionId,
			last_message_id as lastMessageId,
			pending_transaction as pendingTransaction,
			username,
			state,
			timezone,
			currency,
			categories_cache as categoriesCache,
			created_at as createdAt,
			updated_at as updatedAt
		FROM users
		WHERE telegram_id = ?
	`

	row := r.db.QueryRowContext(ctx, query, telegramId)
	var (
		u              domain.User
		spreadsheetID  sql.NullString
		lastTxID       sql.NullInt64
		lastMessageID  sql.NullInt64
		pendingTx      sql.NullString
		username       sql.NullString
		categoriesJSON sql.NullString
		stateStr       string
		timezone       sql.NullString
		currency       sql.NullString
	)

	err := row.Scan(
		&u.ID,
		&u.TelegramID,
		&spreadsheetID,
		&lastTxID,
		&lastMessageID,
		&pendingTx,
		&username,
		&stateStr,
		&timezone,
		&currency,
		&categoriesJSON,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}

		return nil, fmt.Errorf("error scanning user row: %w", err)
	}

	// Map the state string to the UserState type
	u.State = domain.UserState(stateStr)
	if spreadsheetID.Valid {
		u.SpreadsheetID = spreadsheetID.String
	}

	if lastTxID.Valid {
		u.LastTransactionID = int(lastTxID.Int64)
	}

	if lastMessageID.Valid {
		u.LastMessageID = int(lastMessageID.Int64)
	}

	if pendingTx.Valid {
		u.PendingTransaction = pendingTx.String
	}

	if username.Valid {
		u.Username = username.String
	}

	if categoriesJSON.Valid && categoriesJSON.String != "" {
		if err := json.Unmarshal([]byte(categoriesJSON.String), &u.CategoriesCache); err != nil {
			return nil, fmt.Errorf("failed to unmarshal categories: %w", err)
		}
	}

	if timezone.Valid {
		u.Timezone = timezone.String
	}

	if currency.Valid {
		u.Currency = currency.String
	}

	return &u, nil
}

func (r *UserRepository) Upsert(ctx context.Context, user *domain.User) error {
	// Convert the categories cache to JSON for storage
	var categoriesJSON sql.NullString
	if len(user.CategoriesCache) > 0 {
		categoriesBytes, err := json.Marshal(user.CategoriesCache)
		if err != nil {
			return fmt.Errorf("failed to marshal categories (tg_id: %d): %w", user.TelegramID, err)
		}
		categoriesJSON = sql.NullString{String: string(categoriesBytes), Valid: true}
	}

	// Convert pending transaction to JSON for storage
	var pendingTxVal sql.NullString
	if user.PendingTransaction != "" {
		pendingTxVal = sql.NullString{String: user.PendingTransaction, Valid: true}
	}	

	// Handling username and spreadsheet_id as sql.NullString to avoid inserting empty strings
	var usernameVal sql.NullString
	if user.Username != "" {
		usernameVal = sql.NullString{String: user.Username, Valid: true}
	}

	var spreadsheetIDVal sql.NullString
	if user.SpreadsheetID != "" {
		spreadsheetIDVal = sql.NullString{String: user.SpreadsheetID, Valid: true}
	}

	var lastTxIDVal sql.NullInt64
	if user.LastTransactionID > 0 {
		lastTxIDVal = sql.NullInt64{Int64: int64(user.LastTransactionID), Valid: true}
	}

	var lastMessageIDVal sql.NullInt64
	if user.LastMessageID > 0 {
		lastMessageIDVal = sql.NullInt64{Int64: int64(user.LastMessageID), Valid: true}
	}

	// Handling timezone and currency as sql.NullString to avoid inserting empty strings
	var timezoneVal sql.NullString
	if user.Timezone != "" {
		timezoneVal = sql.NullString{String: user.Timezone, Valid: true}
	}

	var currencyVal sql.NullString
	if user.Currency != "" {
		currencyVal = sql.NullString{String: user.Currency, Valid: true}
	}

	query := `
		INSERT INTO users (
			telegram_id, 
			username, 
			state, 
			timezone, 
			currency, 
			spreadsheet_id, 
			last_transaction_id,
			last_message_id,
			pending_transaction,
			categories_cache, 
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(telegram_id) DO UPDATE SET
			username            = excluded.username,
			state               = excluded.state,
			timezone            = COALESCE(excluded.timezone, users.timezone),
			currency            = COALESCE(excluded.currency, users.currency),
			spreadsheet_id      = COALESCE(excluded.spreadsheet_id, users.spreadsheet_id),
			last_transaction_id = COALESCE(excluded.last_transaction_id, users.last_transaction_id, 0),
			last_message_id     = COALESCE(excluded.last_message_id, users.last_message_id, 0),
			categories_cache    = COALESCE(excluded.categories_cache, users.categories_cache),
			pending_transaction = COALESCE(excluded.pending_transaction, users.pending_transaction, ""),
			updated_at          = CURRENT_TIMESTAMP;
	`

	_, err := r.db.ExecContext(ctx, query,
		user.TelegramID,
		usernameVal,
		string(user.State),
		timezoneVal,
		currencyVal,
		spreadsheetIDVal,
		lastTxIDVal,
		lastMessageIDVal,
		pendingTxVal,
		categoriesJSON,
	)

	return err
}

func (r *UserRepository) IncrementLastTransactionID(ctx context.Context, telegramId int64) (int, error) {
	query := `
		UPDATE users
		SET last_transaction_id = COALESCE(last_transaction_id, 0) + 1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
		RETURNING last_transaction_id
	`
	var newID int
	err := r.db.QueryRowContext(ctx, query, telegramId).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("failed to increment last_transaction_id: %w", err)
	}
	return newID, nil
}

func (r *UserRepository) UpdateState(ctx context.Context, telegramId int64, newState domain.UserState) error {
	query := `
		UPDATE users
		SET state = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`
	res, err := r.db.ExecContext(ctx, query, string(newState), telegramId)
	if err != nil {
		return err
	}

	// Checkig if any row was affected to determine if the user exists
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
