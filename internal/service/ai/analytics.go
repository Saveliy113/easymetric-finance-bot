package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"em-finance-bot/internal/service/sheets"

	"google.golang.org/genai"
)

type DateFilterQuery struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Category    string `json:"category"`
	PeriodLabel string `json:"period_label"`
}

type FinancialSummary struct {
	TotalIncome       float64
	TotalExpense      float64
	NetBalance        float64
	CategoryExpenses  map[string]float64
	CategoryBreakdown string
	TransactionsCount int
	Currency          string
}

const extractDateFilterPromptTemplate = `Ты — специализированный парсер временных периодов и параметров для финансового ассистента.

Контекст выполнения:
- Текущая дата и день недели: %s
- Часовой пояс пользователя: %s
- Доступные категории пользователя: [%s]

Твоя задача:
Проанализировать входящий запрос пользователя и определить точные календарные границы периода [start_date, end_date] в формате YYYY-MM-DD, а также категорию расходов, если она упомянута.

Правила расчета дат:
1. "сегодня" -> start_date и end_date равны текущей дате.
2. "вчера" -> start_date и end_date равны вчерашней дате.
3. "на этой неделе" -> от понедельника текущей недели по текущую дату (или воскресенье текущей недели).
4. "на прошлой неделе" -> от понедельника по воскресенье строго предыдущей недели.
5. "в этом месяце" -> с 1-го числа текущего месяца по текущую дату.
6. "в прошлом месяце" -> с 1-го по последнее число предыдущего календарного месяца.
7. "за последние N дней" -> от (сегодня минус N-1 дней) до сегодня включительно.
8. Если указан конкретный месяц (например, "в августе"), используй 1-е и последнее число этого месяца текущего года (%d).
9. Категория (category): сопоставь с одной из доступных категорий пользователя. Если категория явно не указана или запрос общий ("все траты", "сколько потратил") — верни пустую строку "".

Формат ответа:
Верни ИСКЛЮЧИТЕЛЬНО валидный JSON следующей структуры, без ` + "```json" + ` и без сопроводительного текста:
{
  "start_date": "YYYY-MM-DD",
  "end_date": "YYYY-MM-DD",
  "category": "Название категории или пустая строка",
  "period_label": "Понятное человеку название периода (например: 'Прошлая неделя (21.09 — 27.09)')"
}

Входной запрос пользователя:
"%s"
`

const financialAnalyticsPromptTemplate = `Ты — персональный финансовый аналитик бота EM Personal Finances.

Твоя задача:
Изучить агрегированные финансовые показатели пользователя за запрошенный период и составить лаконичный, наглядный и полезный отчет для мессенджера Telegram.

Входные данные:
Период: %s
Общий доход: %s %s
Общий расход: %s %s
Баланс (Доход - Расход): %s %s
Детализация по категориям:
%s

Количество транзакций за период: %d

Правила оформления:
1. Используй ТОЛЬКО HTML-теги для форматирования Telegram: <b>жирный</b>, <i>курсив</i>, <code>код</code>. Никогда не используй Markdown (никаких **, __, ###).
2. Структура ответа:
   - Заголовок с периодом и ключевой метрикой (Итог/Баланс).
   - Краткий список основных трат (топ-3 или топ-5 категорий с суммами и долями).
   - 1 короткое полезное наблюдение или совет (инсайт): обрати внимание на самую большую статью расходов, долю импульсивных трат или соотношение доходов и расходов.
3. Стиль: дружелюбный, лаконичный, без лишней "воды" и морализаторства.
4. Если расходов или данных за период нет (0 операций), вежливо сообщи, что записей за этот промежуток не найдено.
`

func (s *GeminiService) ExtractDateFilter(
	ctx context.Context,
	userQuery string,
	userTZ string,
	categoriesCache string,
) (*DateFilterQuery, error) {
	loc, err := time.LoadLocation(userTZ)
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)

	var categories []string
	if categoriesCache != "" {
		_ = json.Unmarshal([]byte(categoriesCache), &categories)
	}

	prompt := fmt.Sprintf(
		extractDateFilterPromptTemplate,
		now.Format("2006-01-02, Monday"),
		userTZ,
		strings.Join(categories, ", "),
		now.Year(),
		userQuery,
	)

	slog.InfoContext(ctx, "Отправляем запрос в Gemini для извлечения фильтра дат",
		slog.String("query", userQuery),
		slog.String("timezone", userTZ),
	)

	config := &genai.GenerateContentConfig{
		Temperature:      genai.Ptr[float32](0.0),
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"start_date":   {Type: genai.TypeString},
				"end_date":     {Type: genai.TypeString},
				"category":     {Type: genai.TypeString},
				"period_label": {Type: genai.TypeString},
			},
			Required: []string{"start_date", "end_date", "category", "period_label"},
		},
	}

	result, err := s.client.Models.GenerateContent(
		ctx,
		"gemini-3.5-flash-lite",
		genai.Text(prompt),
		config,
	)
	if err != nil {
		return nil, fmt.Errorf("gemini extract date filter failed: %w", err)
	}

	var query DateFilterQuery
	if err := json.Unmarshal([]byte(result.Text()), &query); err != nil {
		return nil, fmt.Errorf("error decoding json response: %w", err)
	}

	slog.InfoContext(ctx, "Фильтр дат успешно извлечен Gemini",
		slog.String("start_date", query.StartDate),
		slog.String("end_date", query.EndDate),
		slog.String("category", query.Category),
		slog.String("period_label", query.PeriodLabel),
	)

	return &query, nil
}

func (s *GeminiService) GenerateFinancialReport(
	ctx context.Context,
	periodLabel string,
	summary *FinancialSummary,
) (string, error) {
	if summary == nil {
		return "", fmt.Errorf("summary is nil")
	}

	prompt := fmt.Sprintf(
		financialAnalyticsPromptTemplate,
		periodLabel,
		formatAmount(summary.TotalIncome),
		summary.Currency,
		formatAmount(summary.TotalExpense),
		summary.Currency,
		formatAmount(summary.NetBalance),
		summary.Currency,
		summary.CategoryBreakdown,
		summary.TransactionsCount,
	)

	slog.InfoContext(ctx, "Отправляем запрос в Gemini для генерации финансового отчета",
		slog.String("period_label", periodLabel),
		slog.Float64("total_income", summary.TotalIncome),
		slog.Float64("total_expense", summary.TotalExpense),
		slog.Int("tx_count", summary.TransactionsCount),
	)

	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0.3),
	}

	result, err := s.client.Models.GenerateContent(
		ctx,
		"gemini-3.5-flash-lite",
		genai.Text(prompt),
		config,
	)
	if err != nil {
		return "", fmt.Errorf("gemini financial report generation failed: %w", err)
	}

	report := strings.TrimSpace(result.Text())
	report = strings.TrimPrefix(report, "```html")
	report = strings.TrimPrefix(report, "```")
	report = strings.TrimSuffix(report, "```")
	report = strings.TrimSpace(report)

	return report, nil
}

func FilterTransactionsByDate(
	transactions []*sheets.Transaction,
	startDate string,
	endDate string,
	categoryFilter string,
) []*sheets.Transaction {
	var filtered []*sheets.Transaction
	categoryFilter = strings.TrimSpace(categoryFilter)

	for _, tx := range transactions {
		if tx == nil || tx.Date.IsZero() {
			continue
		}

		txDateStr := tx.Date.Format("2006-01-02")
		if startDate != "" && txDateStr < startDate {
			continue
		}
		if endDate != "" && txDateStr > endDate {
			continue
		}

		if categoryFilter != "" {
			if !strings.EqualFold(strings.TrimSpace(tx.Category), categoryFilter) {
				continue
			}
		}

		filtered = append(filtered, tx)
	}

	return filtered
}

func AggregateTransactions(transactions []*sheets.Transaction, currency string) *FinancialSummary {
	summary := &FinancialSummary{
		Currency:          currency,
		CategoryExpenses:  make(map[string]float64),
		TransactionsCount: len(transactions),
	}

	for _, tx := range transactions {
		if tx == nil {
			continue
		}
		if tx.Type == sheets.TypeIncome {
			summary.TotalIncome += tx.Amount
		} else {
			summary.TotalExpense += tx.Amount
			cat := strings.TrimSpace(tx.Category)
			if cat == "" {
				cat = "Без категории"
			}
			summary.CategoryExpenses[cat] += tx.Amount
		}
	}

	summary.NetBalance = summary.TotalIncome - summary.TotalExpense

	if len(summary.CategoryExpenses) == 0 {
		summary.CategoryBreakdown = "Нет расходов за выбранный период"
		return summary
	}

	type catItem struct {
		Name   string
		Amount float64
		Pct    float64
	}

	var items []catItem
	for cat, amt := range summary.CategoryExpenses {
		pct := 0.0
		if summary.TotalExpense > 0 {
			pct = (amt / summary.TotalExpense) * 100
		}
		items = append(items, catItem{
			Name:   cat,
			Amount: amt,
			Pct:    pct,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Amount > items[j].Amount
	})

	var lines []string
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- %s: %s %s (%.0f%%)",
			item.Name,
			formatAmount(item.Amount),
			currency,
			item.Pct,
		))
	}

	summary.CategoryBreakdown = strings.Join(lines, "\n")
	return summary
}

func formatAmount(amount float64) string {
	isNegative := amount < 0
	if isNegative {
		amount = -amount
	}

	cents := int64(amount*100 + 0.5)
	intPart := cents / 100
	fracPart := cents % 100

	str := strconv.FormatInt(intPart, 10)
	var result []rune
	n := len(str)
	for i, r := range str {
		if i > 0 && (n-i)%3 == 0 {
			result = append(result, ' ')
		}
		result = append(result, r)
	}
	resStr := string(result)
	if fracPart > 0 {
		resStr += fmt.Sprintf(".%02d", fracPart)
	}
	if isNegative {
		resStr = "-" + resStr
	}
	return resStr
}
