package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

const CityResolutionPrompt = `
Ты системный анализатор географических названий. 
Тебе передано название города или населенного пункта: "%s".

Твоя задача — вернуть корректный JSON следующего формата:
{
  "city": "Каноническое название города на русском",
  "timezone": "Идентификатор таймзоны IANA (например Asia/Almaty, Europe/Moscow)",
  "currency": "Трехбуквенный международный код базовой валюты (например KZT, RUB, USD, EUR)",
  "is_valid": true
}

Правила:
1. Если переданный текст не является городом или распознать невозможно, верни "is_valid": false.
2. Верни ТОЛЬКО валидный JSON-объект без markdown-разметки, без тройных кавычек backticks и без лишнего текста.
`

type LocationInfo struct {
	City     string `json:"city"`
	Timezone string `json:"timezone"`
	Currency string `json:"currency"`
	IsValid  bool   `json:"is_valid"`
}

type GeminiService struct {
	client *genai.Client
}

func NewGeminiService(ctx context.Context, apiKey string) *GeminiService {
	// Initializing new client
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})

	if err != nil {
		panic("Failed to initialize Gemini client: " + err.Error())
	}

	return &GeminiService{
		client: client,
	}
}

func (s *GeminiService) ParseCity(ctx context.Context, cityName string) (*LocationInfo, error) {
	prompt := fmt.Sprintf(CityResolutionPrompt, cityName)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
	}

	result, err := s.client.Models.GenerateContent(
		ctx,
		"gemini-3.5-flash-lite",
		genai.Text(prompt),
		config,
	)
	if err != nil {
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}

	rawText := strings.TrimSpace(result.Text())
	if rawText == "" {
		return nil, fmt.Errorf("empty response from gemini")
	}

	var info LocationInfo
	if err := json.Unmarshal([]byte(rawText), &info); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w (raw: %s)", err, rawText)
	}

	return &info, nil
}
