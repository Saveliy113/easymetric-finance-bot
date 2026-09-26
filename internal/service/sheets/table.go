package sheets

import (
	"context"
	"em-finance-bot/internal/domain"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type SheetsService struct {
	srv *sheets.Service
}

// TODO: Move to domain maybe
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

type FoundTransaction struct {
	RowIndex    int          // Row number in google sheets
	Transaction *Transaction // Transaction
}

// FindTransactionByID searches transaction by it's id in table E column
func (s *SheetsService) FindTransactionByID(ctx context.Context, spreadsheetID string, targetID int) (*FoundTransaction, error) {
	// Reading journal operations (starting from row 3, where data begins)
	readRange := "'Дашборд'!E3:J"
	resp, err := s.srv.Spreadsheets.Values.Get(spreadsheetID, readRange).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("[SheetsService.FindTransactionByID] get range %s: %w", readRange, err)
	}

	targetIDStr := strconv.Itoa(targetID)

	// Searching from the end, as recent operations are at the bottom
	for i := len(resp.Values) - 1; i >= 0; i-- {
		row := resp.Values[i]
		if len(row) == 0 {
			continue
		}

		// Column E is row[0] (ID)
		if fmt.Sprint(row[0]) == targetIDStr {
			tx := &Transaction{
				ID: int64(targetID),
			}

			// Transaction date
			if len(row) > 1 {
				tx.Date, _ = time.Parse("2006-01-02 15:04", fmt.Sprint(row[1]))
			}

			// Category and type
			if len(row) > 2 {
				tx.Category = fmt.Sprint(row[2])
			}
			if len(row) > 3 {
				tx.Type = TransactionType(fmt.Sprint(row[3]))
			}

			// Transaction amount
			if len(row) > 4 {
				cleanAmount := strings.ReplaceAll(fmt.Sprint(row[4]), ",", "")
				tx.Amount, _ = strconv.ParseFloat(cleanAmount, 64)
			}

			// Transaction description
			if len(row) > 5 {
				tx.Description = fmt.Sprint(row[5])
			}

			return &FoundTransaction{
				RowIndex:    i + 3,
				Transaction: tx,
			}, nil
		}
	}

	return nil, domain.ErrTransactionNotFound
}

func NewSheetService(ctx context.Context, credentialsFilePath string) *SheetsService {
	srv, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsFilePath))
	if err != nil {
		panic("Failed to create sheets service: " + err.Error())
	}

	return &SheetsService{srv: srv}
}

func (s *SheetsService) ValidateAccess(ctx context.Context, spreadsheetID string) error {
	slog.InfoContext(ctx, "Проверяем доступ к таблице", slog.String("spreadsheetID", spreadsheetID))

	// Trying to get table metadata
	call := s.srv.Spreadsheets.Get(spreadsheetID).Fields("spreadsheetId")
	_, err := call.Context(ctx).Do()

	if err != nil {
		var gErr *googleapi.Error
		if errors.As(err, &gErr) {
			switch gErr.Code {
			case http.StatusNotFound:
				return domain.ErrSheetNotFound
			case http.StatusForbidden:
				return domain.ErrSheetAccessDenied
			default:
				return domain.ErrGoogleAPIFailed
			}
		}
		return fmt.Errorf("failed to validate table access: %w", err)
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
		return domain.ErrInvalidCategories
	}

	// Getting table metadata for defining sheet ID and name
	slog.InfoContext(ctx, "Получаем метаданные таблицы по ID и имени листа", slog.String("spreadsheetID", spreadsheetID), slog.String("sheetName", sheetName))
	ss, err := s.srv.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		slog.InfoContext(ctx, "Не удалось получить таблицу", slog.Any("error", err))
		return fmt.Errorf("error getting access to table: %w", err)
	}

	var targetSheetID int64
	actualSheetName := ss.Sheets[0].Properties.Title

	// Searching for a sheet with target sheet name (or getting the first one)
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

	// Generating rows: Categories (A) | Summ formula (B) | Share formula (C)
	var rows [][]interface{}
	for i, cat := range categories {
		row := startRow + i

		// Summ formula:
		// Calculate sum from column I, where:
		// - type (column H) = "Расход"
		// - category (column G) = current category A{row}
		// - date (column F) in period of current month from B3 and B4
		formulaSum := fmt.Sprintf(
			`=SUMIFS(I:I, H:H, "Расход", G:G, A%d, F:F, ">="&DATE($B$3,$B$4,1), F:F, "<"&EDATE(DATE($B$3,$B$4,1),1))`,
			row,
		)

		// Share formula:
		// Calculate share of current category from total sum in cell B11
		formulaShare := fmt.Sprintf(`=IFERROR(B%d / $B$11, 0)`, row)

		rows = append(rows, []interface{}{
			cat,          // Category (column A)
			formulaSum,   // Summ formula (column B)
			formulaShare, // Share formula (column C)
		})
	}

	// Clearing old rows with categories with reserve (A12:C60)
	clearRange := fmt.Sprintf("'%s'!A12:C60", actualSheetName)
	_, _ = s.srv.Spreadsheets.Values.Clear(spreadsheetID, clearRange, &sheets.ClearValuesRequest{}).
		Context(ctx).
		Do()

	// Writing generated categories and formulas
	targetRange := fmt.Sprintf("'%s'!A%d:C%d", actualSheetName, startRow, endRow)
	valueRange := &sheets.ValueRange{
		Values: rows,
	}

	_, err = s.srv.Spreadsheets.Values.Update(spreadsheetID, targetRange, valueRange).
		ValueInputOption("USER_ENTERED").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("error while inserting rows: %w", err)
	}

	// Updating data validation list in column G (Category)
	// List contains "Доход" first + all user categories
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
						StartRowIndex:    2,
						EndRowIndex:      10000000,
						StartColumnIndex: 6,
						EndColumnIndex:   7,
					},
					Rule: &sheets.DataValidationRule{
						Condition: &sheets.BooleanCondition{
							Type:   "ONE_OF_LIST",
							Values: conditionValues,
						},
						ShowCustomUi: true,
						Strict:       false,
					},
				},
			},
		},
	}

	_, err = s.srv.Spreadsheets.BatchUpdate(spreadsheetID, batchReq).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("error while updating data validation list: %w", err)
	}

	slog.InfoContext(ctx, "Категории, формулы сумм и выпадающие списки успешно настроены",
		slog.String("sheet", actualSheetName),
		slog.Int("total_rows", totalCats),
	)

	return nil
}

func (s *SheetsService) SaveTransaction(ctx context.Context, spreadsheetID string, transaction *Transaction) error {
	if transaction == nil {
		return fmt.Errorf("transaction cannot be empty")
	}

	// Formating date for column F: YYYY-MM-DD HH:MM
	dateStr := transaction.Date.Format("2006-01-02 15:04:05")

	// Determining display type and category
	var displayType string
	var finalCategory string

	if transaction.Type == "income" || transaction.Type == "Доход" {
		displayType = "Доход"
		finalCategory = "Доход"
	} else {
		displayType = "Расход"
		finalCategory = transaction.Category
	}

	// Formating row values according columns: E (ID), F (Дата), G (Категория), H (Тип), I (Сумма), J (Описание)
	rowValues := []interface{}{
		transaction.ID,          // E: ID
		dateStr,                 // F: Date (e.g., 2026-08-12 15:04:05)
		finalCategory,           // G: Category ("Доход" or expense category)
		displayType,             // H: Type ("Расход" / "Доход")
		transaction.Amount,      // I: Numerical amount without currency (e.g., 12500)
		transaction.Description, // J: Description
	}

	// Range for adding transaction to the logbook on the "Dashboard" sheet
	targetRange := "'Дашборд'!E3:J"

	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{rowValues},
	}

	slog.InfoContext(ctx, "Добавляем строку транзакции в таблицу",
		slog.Int64("id", transaction.ID),
		slog.String("spreadsheet_id", spreadsheetID),
		slog.String("range", targetRange),
		slog.String("category", finalCategory),
		slog.Float64("amount", transaction.Amount),
	)

	// Append finds the first free row in E3:J and writes data without shifting/inserting rows across the sheet
	_, err := s.srv.Spreadsheets.Values.Append(spreadsheetID, targetRange, valueRange).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("OVERWRITE").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("error appending row to table: %w", err)
	}

	slog.InfoContext(ctx, "Транзакция успешно записана в журнал",
		slog.Int64("id", transaction.ID),
		slog.String("type", displayType),
		slog.Float64("amount", transaction.Amount),
		slog.String("category", finalCategory),
	)

	return nil
}
