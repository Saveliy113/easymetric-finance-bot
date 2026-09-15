package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

func ParseTransactionDate(raw string, userTZ string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("пустая дата транзакции")
	}

	loc, err := time.LoadLocation(userTZ)
	if err != nil {
		slog.Warn("Не удалось загрузить таймзону пользователя, используется UTC", "таймзона", userTZ, "ошибка", err)
		loc = time.UTC
	}

	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}

	for _, layout := range []string{"2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z07:00"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}

	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("не удалось разобрать дату %q", raw)
}

type ParsedTransaction struct {
	IsValid             bool     `json:"is_valid"`
	Type                string   `json:"type"` // "expense" или "income"
	Amount              float64  `json:"amount"`
	Category            string   `json:"category"`
	Description         string   `json:"description"`
	Date                string   `json:"date"`
	NeedsClarification  bool     `json:"needs_clarification"`
	SuggestedCategories []string `json:"suggested_categories"`
}

const audioTranscriptionPropmt = `
Точно расшифруй эту голосовую аудиозапись в обычный текст.
Аудио содержит информацию о личных финансах или повседневных тратах на русском или смешанном языке.
Выведи ТОЛЬКО расшифрованный текст без вступительных фраз, кавычек, временных меток и пояснений.
`

const parseTransactionPrompt = `
ы — финансовый ассистент, который преобразует сообщения пользователя в структурированные финансовые транзакции для таблицы личных финансов.

КОНТЕКСТ И ПРАВИЛА:
1. Текущие локальные дата и время: %s (Часовой пояс: %s).
2. Валюта по умолчанию: %s.
3. Доступные категории РАСХОДОВ: %s.
4. Типы операций:
   - "expense" (траты, покупки, подписки, переводы другим).
   - "income" (доходы, зарплата, кэшбэк, возврат долга, пополнения).

ПРАВИЛА ОПРЕДЕЛЕНИЯ ПОЛЕЙ:
1. "is_valid": true, если есть сумма и финансовый смысл. Если это просто текст, спам или нет суммы — false.
2. "type": "expense" или "income".
3. "amount": положительное число (float). Если валюта не названа — считаем, что это %s.
4. "category":
   - Для "expense": выбери НАИБОЛЕЕ подходящую категорию СТРОГО из списка доступных категорий.
   - Для "income": оставь пустую строку "".
5. "description": краткое понятное назначение платежа на русском языке (например: "Кофе", "Метро", "Аренда жилья"). Не дублируй сюда сумму.
6. "date": дата в формате ISO 8601 с часовым поясом, например 2026-09-14T21:25:57+03:00 или 2026-09-14T21:25:57Z. Если часовой пояс не указан явно, используй локальное время пользователя: %s.
7. "needs_clarification" и "suggested_categories":
   - Если операция — расход, но ее нельзя точно сопоставить с одной категорией (например, "Apple 5000" или "Перевод 2000"):
     * "needs_clarification": true
     * "category": ""
     * "suggested_categories": массив из 2-3 наиболее вероятных категорий из списка.
   - Если всё однозначно:
     * "needs_clarification": false
     * "suggested_categories": []

Входное сообщение пользователя:
"%s"
`

func (s *GeminiService) TranscribeVoice(ctx context.Context, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("Не удалось расщифровать голосовое сообщение - сообщение пустое")
	}

	result, err := s.client.Models.GenerateContent(
		ctx,
		"gemini-3.5-flash-lite",
		genai.Text(audioTranscriptionPropmt),
		nil,
	)

	if err != nil {
		return "", fmt.Errorf("Ошибка при распозновании голосового сообщения: %w", err)
	}

	transcription := strings.TrimSpace(result.Text())
	if transcription == "" {
		return "", fmt.Errorf("Сообщение пустое после попытки расшифровки. Попробуйте еще раз")
	}

	return transcription, nil
}

func (s *GeminiService) ParsedTransaction(
	ctx context.Context,
	rawText string,
	categories []string,
	userCurrency string,
	userTZ string,
) (*ParsedTransaction, error) {
	loc, err := time.LoadLocation(userTZ)
	if err != nil {
		slog.Warn("Не удалось загрузить таймзону пользователя, используется UTC", "таймзона", userTZ, "ошибка", err)
		loc = time.UTC
	}
	now := time.Now().In(loc)

	prompt := fmt.Sprintf(
		parseTransactionPrompt,
		now.Format("2006-01-02 15:04:05"),
		userTZ,
		userCurrency,
		strings.Join(categories, ", "),
		userCurrency,
		userTZ,
		rawText,
	)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"is_valid": {Type: genai.TypeBoolean},
				"type": {
					Type: genai.TypeString,
					Enum: []string{"expense", "income"},
				},
				"amount":              {Type: genai.TypeNumber},
				"category":            {Type: genai.TypeString},
				"description":         {Type: genai.TypeString},
				"date":                {Type: genai.TypeString},
				"needs_clarification": {Type: genai.TypeBoolean},
				"suggested_categories": {
					Type:  genai.TypeArray,
					Items: &genai.Schema{Type: genai.TypeString},
				},
			},
			Required: []string{
				"is_valid",
				"type",
				"amount",
				"category",
				"description",
				"date",
				"needs_clarification",
				"suggested_categories",
			},
		},
	}

	result, err := s.client.Models.GenerateContent(ctx, "gemini-3.5-flash-lite", genai.Text(prompt), config)
	if err != nil {
		return nil, fmt.Errorf("ошибка анализа транзакции: %w", err)
	}

	var transaction ParsedTransaction
	if err := json.Unmarshal([]byte(result.Text()), &transaction); err != nil {
		return nil, fmt.Errorf("ошибка разбора JSON ответа: %w", err)
	}

	return &transaction, nil
}
