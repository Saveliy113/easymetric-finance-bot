package ai

import (
	"context"
	"em-finance-bot/internal/service/sheets"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

type ParsedTransaction struct {
	IsValid             bool      `json:"is_valid"`
	Type                string    `json:"type"`
	Amount              float64   `json:"amount"`
	Category            string    `json:"category"`
	Description         string    `json:"description"`
	Date                time.Time `json:"date"`
	NeedsClarification  bool      `json:"needs_clarification"`
	SuggestedCategories []string  `json:"suggested_categories"`
}

const audioTranscriptionPropmt = `
Точно расшифруй эту голосовую аудиозапись в обычный текст.
Аудио содержит информацию о личных финансах или повседневных тратах на русском или смешанном языке.
Выведи ТОЛЬКО расшифрованный текст без вступительных фраз, кавычек, временных меток и пояснений.
`

const parseTransactionPrompt = `
Ты — финансовый ассистент, который преобразует сообщения пользователя в структурированные финансовые транзакции для таблицы личных финансов.

КОНТЕКСТ:
1. Текущие локальные дата и время: %s (Часовой пояс: %s).
2. Валюта по умолчанию: %s.
3. Доступные категории РАСХОДОВ: %s.
4. Типы операций:
   - "expense" (траты, покупки, услуги, аренда, переводы).
   - "income" (доходы, зарплата, кэшбэк, пополнения).

ОБЯЗАТЕЛЬНЫЙ АЛГОРИТМ ОПРЕДЕЛЕНИЯ КАТЕГОРИИ РАСХОДА:
В поле "category_analysis" ты ОБЯЗАН выполнить внутреннюю самопроверку по шагам:

Шаг 1. Перебор кандидатов:
Посмотри на список доступных категорий и найди ВСЕ категории, к которым данная операция может подходить хотя бы теоретически (с учетом разных мотивов пользователя).

Шаг 2. Контрольный вопрос на неоднозначность:
Задай себе вопрос: «Зависит ли выбор между найденными категориями от личной цели пользователя, контекста или его настроения?»
- Если подходит 2 или более категорий из доступного списка:
  -> Это Связь 1:N. Запрещено выбирать одну!
  -> "needs_clarification": true, "category": "", "suggested_categories": [2-3 найденные категории].
- Если подходит строго 1 категория и никакая другая даже теоретически не имеет отношения:
  -> Это Связь 1:1.
  -> "needs_clarification": false, "category": точное название, "suggested_categories": [].
- Если не подходит ни одна категория из списка:
  -> Это Связь 0.
  -> "needs_clarification": true, "category": "", "suggested_categories": [].

ОСТАЛЬНЫЕ ПОЛЯ:
1. "is_valid": true, если есть сумма и финансовый смысл. Если спам или нет суммы — false.
2. "type": "expense" или "income". Для "income": "category": "", "needs_clarification": false, "suggested_categories": [].
3. "amount": положительное число (float). Если валюта не названа — считаем, что это %s.
4. "description": краткое понятное назначение платежа на языке сообщения (без суммы).
5. "date": дата в формате ISO 8601 с часовым поясом (например, 2026-09-14T21:25:57+03:00). Если часовой пояс не указан явно, используй локальное время пользователя: %s.

Входное сообщение пользователя:
"%s"
`

func (s *GeminiService) TranscribeVoice(ctx context.Context, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("voice message data is empty")
	}

	slog.InfoContext(ctx, "Отправляем аудио в Gemini для транскрипции", slog.Int("bytes_len", len(data)))

	result, err := s.client.Models.GenerateContent(
		ctx,
		"gemini-3.5-flash-lite",
		[]*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{
						Text: audioTranscriptionPropmt,
					},
					{
						InlineData: &genai.Blob{
							MIMEType: "audio/ogg",
							Data:     data,
						},
					},
				},
			},
		},
		nil,
	)

	if err != nil {
		return "", fmt.Errorf("error recognizing voice message: %w", err)
	}

	transcription := strings.TrimSpace(result.Text())
	if transcription == "" {
		return "", fmt.Errorf("empty transcription from gemini")
	}

	slog.InfoContext(ctx, "Голосовое сообщение успешно расшифровано Gemini", slog.String("transcription", transcription))

	return transcription, nil
}

func (s *GeminiService) ParseTransaction(
	ctx context.Context,
	rawText string,
	categories []string,
	userCurrency string,
	userTZ string,
) (*ParsedTransaction, error) {
	loc, err := time.LoadLocation(userTZ)
	if err != nil {
		return nil, fmt.Errorf("failed to load user timezone %q: %w", userTZ, err)
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
		Temperature:      genai.Ptr[float32](0.0),
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"category_analysis": {
					Type:        genai.TypeString,
					Description: "Внутренний анализ: перечисли подходящие категории из списка и ответь, есть ли неоднозначность выбора.",
				},
				"is_valid": {Type: genai.TypeBoolean},
				"type": {
					Type: genai.TypeString,
					Enum: []string{"expense", "income"},
				},
				"amount":              {Type: genai.TypeNumber},
				"category":            {Type: genai.TypeString},
				"description":         {Type: genai.TypeString},
				"date":                {Type: genai.TypeString, Format: "date-time"},
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

	slog.InfoContext(ctx, "Отправляем запрос в Gemini для парсинга транзакции",
		slog.String("rawText", rawText),
		slog.String("currency", userCurrency),
		slog.String("timezone", userTZ),
	)

	result, err := s.client.Models.GenerateContent(ctx, "gemini-3.5-flash-lite", genai.Text(prompt), config)
	if err != nil {
		return nil, fmt.Errorf("error analyzing transaction: %w", err)
	}

	var transaction ParsedTransaction
	if err := json.Unmarshal([]byte(result.Text()), &transaction); err != nil {
		return nil, fmt.Errorf("error decoding json response: %w", err)
	}

	slog.InfoContext(ctx, "Транзакция успешно проанализирована Gemini",
		slog.Bool("is_valid", transaction.IsValid),
		slog.String("type", transaction.Type),
		slog.Float64("amount", transaction.Amount),
		slog.String("category", transaction.Category),
	)

	return &transaction, nil
}

func (p *ParsedTransaction) ToTransaction(transactionId int64, userID int64) *sheets.Transaction {
	return &sheets.Transaction{
		ID:          transactionId,
		UserID:      userID,
		Type:        sheets.TransactionType(p.Type),
		Amount:      p.Amount,
		Category:    p.Category,
		Description: p.Description,
		Date:        p.Date,
		CreatedAt:   time.Now(),
	}
}
