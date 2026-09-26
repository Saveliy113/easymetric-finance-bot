package domain

import "errors"

var (
	// User errors
	ErrUserNotFound = errors.New("user not found")

	// City errors
	ErrInvalidCity = errors.New("invalid or empty city")
	ErrParsingCity = errors.New("error while getting timezone and city from the Gemini API")

	// Categories errors
	ErrInvalidCategories = errors.New("invalid or empty categories")
	ErrParsingCategories = errors.New("error while parsing categories from the Gemini API")

	// Google Sheets errors
	ErrInvalidSheetURL    = errors.New("invalid google sheet url")
	ErrSheetNotFound      = errors.New("google sheet not found")
	ErrSheetAccessDenied  = errors.New("google sheet access denied")
	ErrGoogleAPIFailed    = errors.New("google api access error")
	ErrSheetNotConfigured = errors.New("google sheet is not configured")

	// Transaction & Voice errors
	ErrVoiceDownloadFailed             = errors.New("failed to download voice message")
	ErrVoiceTranscriptionFailed        = errors.New("failed to transcribe voice message")
	ErrEmptyTransaction                = errors.New("empty transaction text")
	ErrInvalidTransaction              = errors.New("failed to parse transaction or amount")
	ErrInvalidTransactionDate          = errors.New("failed to parse transaction date")
	ErrTransactionClarificationExpired = errors.New("transaction clarification has expired")
	ErrTransactionNotFound             = errors.New("transaction not found")
	ErrInvalidTransactionAmount        = errors.New("invalid transaction amount")
	ErrEmptyTransactionDescription     = errors.New("empty transaction description")
)
