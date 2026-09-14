package sheets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type SheetsService struct {
	srv *sheets.Service
}

type TransactionType string

const (
	TypeExpense TransactionType = "expense" // Расход
	TypeIncome  TransactionType = "income"  // Доход
)

// Transaction представляет финансовую операцию пользователя
type Transaction struct {
	ID          int64           `json:"id" db:"id"`
	UserID      int64           `json:"user_id" db:"user_id"`         // Telegram User ID
	Type        TransactionType `json:"type" db:"type"`               // "expense" или "income"
	Amount      float64         `json:"amount" db:"amount"`           // Числовая сумма без знака валюты
	Category    string          `json:"category" db:"category"`       // Категория расхода или "Доход"
	Description string          `json:"description" db:"description"` // Описание (например, "Обед с коллегами")
	Date        time.Time       `json:"date" db:"date"`               // Дата и время совершения операции
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`   // Время создания записи в БД
}

func NewSheetService(ctx context.Context, credentialsFilePath string) *SheetsService {
	srv, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsFilePath))
	if err != nil {
		panic("Failed to create sheets service: " + err.Error())
	}

	return &SheetsService{srv: srv}
}

func (s *SheetsService) ValidateAccess(ctx context.Context, spreadsheetID string) error {
	// Trying to get table metadata
	call := s.srv.Spreadsheets.Get(spreadsheetID).Fields("spreadsheetId")
	_, err := call.Context(ctx).Do()

	if err != nil {
		var gErr *googleapi.Error
		if errors.As(err, &gErr) {
			switch gErr.Code {
			case http.StatusNotFound:
				return errors.New("таблица не найдена, проверь ссылку")
			case http.StatusForbidden:
				return errors.New("нет доступа: добавь почту сервисного аккаунта в 'Редакторы'")
			default:
				return errors.New("ошибка доступа к Google API")
			}
		}
		return err
	}

	return nil
}

func (s *SheetsService) SetupUserCategories(
	ctx context.Context,
	spreadsheetID string,
	sheetName string,
	categories []string,
) error {
	if len(categories) == 0 {
		return fmt.Errorf("список категорий пуст")
	}

	// 1. Получаем метаданные таблицы для точного определения имени и ID листа
	ss, err := s.srv.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		slog.Error("Не удалось получить таблицу", "ошибка", err)
		return fmt.Errorf("ошибка доступа к таблице: %w", err)
	}

	var targetSheetID int64
	actualSheetName := ss.Sheets[0].Properties.Title

	// Ищем лист с именем sheetName (или берем первый лист)
	for _, sheet := range ss.Sheets {
		if sheetName != "" && sheet.Properties.Title == sheetName {
			targetSheetID = sheet.Properties.SheetId
			actualSheetName = sheet.Properties.Title
			break
		}
	}

	startRow := 12
	totalCats := len(categories)
	endRow := startRow + totalCats - 1

	// 2. Генерируем строки: Категория (A) | Формула суммы (B) | Формула доли (C)
	var rows [][]interface{}
	for i, cat := range categories {
		row := startRow + i

		// Формула для колонки B:
		// Считает сумму из H:H, где категория = A{row}, тип = "Расход", а Месяц/Год = YYYY-MM из фильтра B3 и B4
		formulaSum := fmt.Sprintf(
			`=SUMIFS(H:H, F:F, A%d, G:G, "Расход", I:I, TEXT(DATE($B$3, $B$4, 1), "yyyy-mm"))`,
			row,
		)

		// Формула для колонки C:
		// Доля текущей категории от общей суммы ИТОГО в ячейке B11
		formulaShare := fmt.Sprintf(`=IFERROR(B%d / $B$11, 0)`, row)

		rows = append(rows, []interface{}{
			cat,          // Колонка A
			formulaSum,   // Колонка B
			formulaShare, // Колонка C
		})
	}

	// 3. Очищаем старые строки категорий с запасом (A12:C60)
	clearRange := fmt.Sprintf("'%s'!A12:C60", actualSheetName)
	_, _ = s.srv.Spreadsheets.Values.Clear(spreadsheetID, clearRange, &sheets.ClearValuesRequest{}).
		Context(ctx).
		Do()

	// 4. Записываем сгенерированные категории и формулы
	targetRange := fmt.Sprintf("'%s'!A%d:C%d", actualSheetName, startRow, endRow)
	valueRange := &sheets.ValueRange{
		Values: rows,
	}

	_, err = s.srv.Spreadsheets.Values.Update(spreadsheetID, targetRange, valueRange).
		ValueInputOption("USER_ENTERED"). // Обязательно USER_ENTERED для расчета формул
		Context(ctx).
		Do()
	if err != nil {
		slog.Error("Не удалось записать категории и формулы", "диапазон", targetRange, "ошибка", err)
		return fmt.Errorf("ошибка записи строк: %w", err)
	}

	// 5. Обновляем выпадающий список (Data Validation) в колонке F
	// Селект содержит "Доход" первым пунктом + все категории пользователя
	dropdownItems := append([]string{"Доход"}, categories...)
	var conditionValues []*sheets.ConditionValue
	for _, item := range dropdownItems {
		conditionValues = append(conditionValues, &sheets.ConditionValue{
			UserEnteredValue: item,
		})
	}

	batchReq := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				SetDataValidation: &sheets.SetDataValidationRequest{
					Range: &sheets.GridRange{
						SheetId:          targetSheetID,
						StartRowIndex:    2,    // Начиная с 3-й строки (0-based)
						EndRowIndex:      1000, // Вниз по журналу
						StartColumnIndex: 5,    // Колонка F (0-based)
						EndColumnIndex:   6,
					},
					Rule: &sheets.DataValidationRule{
						Condition: &sheets.BooleanCondition{
							Type:   "ONE_OF_LIST",
							Values: conditionValues,
						},
						ShowCustomUi: true,
						Strict:       false, // Чтобы не блокировать ввод при мелких расхождениях
					},
				},
			},
		},
	}

	_, err = s.srv.Spreadsheets.BatchUpdate(spreadsheetID, batchReq).Context(ctx).Do()
	if err != nil {
		slog.Warn("Не удалось обновить выпадающий список в F", "ошибка", err)
	}

	slog.Info("Категории, формулы сумм и выпадающие списки успешно настроены",
		"лист", actualSheetName,
		"строк", totalCats,
	)

	return nil
}

func (s *SheetsService) SaveTransaction(ctx context.Context, spreadsheetID string, transaction *Transaction) error {
	if transaction == nil {
		return fmt.Errorf("транзакция не может быть nil")
	}

	// 1. Форматируем дату операции для колонки E: YYYY-MM-DD
	dateStr := transaction.Date.Format("2006-01-02")

	// 2. Форматируем Месяц/Год для колонки I: YYYY-MM
	monthYearStr := transaction.Date.Format("2006-01")

	// 3. Определяем отображаемый тип и категорию
	var displayType string
	var finalCategory string

	if transaction.Type == "income" || transaction.Type == "Доход" {
		displayType = "Доход"
		finalCategory = "Доход"
	} else {
		displayType = "Расход"
		finalCategory = transaction.Category
	}

	// 4. Формируем строку строго по колонкам E, F, G, H, I, J
	rowValues := []interface{}{
		dateStr,                 // E: Дата (например, 2026-08-12)
		finalCategory,           // F: Категория ("Доход" или категория расхода)
		displayType,             // G: Тип ("Расход" / "Доход")
		transaction.Amount,      // H: Числовая сумма без валюты (например, 12500)
		monthYearStr,            // I: Месяц/Год (например, 2026-08)
		transaction.Description, // J: Описание
	}

	// Диапазон добавления в журнал на листе "Дашборд"
	targetRange := "'Дашборд'!E:J"

	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{rowValues},
	}

	slog.Debug("Сохранение транзакции в Google Таблицу",
		"таблица_id", spreadsheetID,
		"тип", displayType,
		"сумма", transaction.Amount,
		"категория", finalCategory,
	)

	// Append находит первую свободную строку после шапки журнала (строка 3) и вставляет данные
	_, err := s.srv.Spreadsheets.Values.Append(spreadsheetID, targetRange, valueRange).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	if err != nil {
		slog.Error("Ошибка при сохранении транзакции в таблицу",
			"ошибка", err,
			"таблица_id", spreadsheetID,
			"диапазон", targetRange,
		)
		return fmt.Errorf("ошибка добавления строки в таблицу: %w", err)
	}

	slog.Info("Транзакция успешно записана в журнал",
		"тип", displayType,
		"сумма", transaction.Amount,
		"категория", finalCategory,
	)

	return nil
}
