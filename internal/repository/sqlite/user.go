package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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
			// If no user is found, return nil without an error
			return nil, nil
		}
		return nil, err
	}

	// Map the state string to the UserState type
	u.State = domain.UserState(stateStr)
	if spreadsheetID.Valid {
		u.SpreadsheetID = spreadsheetID.String
	}

	if username.Valid {
		u.Username = username.String
	}

	if categoriesJSON.Valid && categoriesJSON.String != "" {
		_ = json.Unmarshal([]byte(categoriesJSON.String), &u.CategoriesCache)
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
			return err
		}
		categoriesJSON = sql.NullString{String: string(categoriesBytes), Valid: true}
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
			categories_cache, 
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(telegram_id) DO UPDATE SET
			username         = excluded.username,
			state            = excluded.state,
			timezone         = COALESCE(excluded.timezone, users.timezone),
			currency         = COALESCE(excluded.currency, users.currency),
			spreadsheet_id   = COALESCE(excluded.spreadsheet_id, users.spreadsheet_id),
			categories_cache = COALESCE(excluded.categories_cache, users.categories_cache),
			updated_at       = CURRENT_TIMESTAMP;
	`

	_, err := r.db.ExecContext(ctx, query,
		user.TelegramID,
		usernameVal,
		string(user.State),
		timezoneVal,
		currencyVal,
		spreadsheetIDVal,
		categoriesJSON,
	)

	return err
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
