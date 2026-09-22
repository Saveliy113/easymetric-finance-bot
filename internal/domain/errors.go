package domain

import "errors"

var (
	ErrInvalidCity = errors.New("invalid or empty city")
	ErrParsingCity = errors.New("error while getting timezone and city from the Gemini API")
)