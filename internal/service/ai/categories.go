package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"
)

const categoriesParsingPrompt = `
Ты — финансовый ассистент, помогающий настроить личный бюджет.
Твоя задача — проанализировать список категорий расходов, который пользователь ввел вручную.

ПРАВИЛА ВАЛИДАЦИИ:
1. Текст должен представлять собой перечень осмысленных сфер человеческих расходов (например: "Еда, Авто, Дети, Спорт", "Кофе, Такси, Игры").
2. Текст НЕ является валидным, если:
   - Это случайный набор символов, бессмысленный текст ("аоылва", "test123", "привет как дела").
   - Это попытка ввести конкретную транзакцию ("купил хлеб 500", "такси вчера").
   - Список слишком короткий (меньше 2 категорий).
   - В тексте присутствуют оскорбления, спам или не относящиеся к финансам предложения.
3. Если ввод валиден:
   - Установи "is_valid": true.
   - Очисти названия: каждое название категории должно начинаться с заглавной буквы, без точек, лишних цифр и спецсимволов.
   - Удали дубликаты.
   - Заполни массив "categories". Поле "error_message" оставь пустым.
4. Если ввод невалиден:
   - Установи "is_valid": false.
   - В поле "error_message" верни короткую, вежливую подсказку на русском языке (1-2 предложения), объясняющую проблему и показывающую пример правильного ввода. Массив "categories" сделай пустым.

Входной текст пользователя:
"""%s"""
`

type CategoriesResponse struct {
	IsValid      bool     `json:"isValid"`
	Categories   []string `json:"categories"`
	ErrorMessage string   `json:"errorMessage"`
}

func (s *GeminiService) ParseCategories(ctx context.Context, categories string) (*CategoriesResponse, error) {
	slog.InfoContext(ctx, "Отправляем запрос в Gemini для валидации категорий", slog.String("categories", categories))

	prompt := fmt.Sprintf(categoriesParsingPrompt, categories)

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

	var info CategoriesResponse
	if err := json.Unmarshal([]byte(rawText), &info); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w (raw: %s)", err, rawText)
	}

	slog.InfoContext(ctx, "Категории успешно проанализированы Gemini",
		slog.Bool("is_valid", info.IsValid),
		slog.Any("categories", info.Categories),
	)

	return &info, nil
}
