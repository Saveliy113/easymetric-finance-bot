package sheets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type SheetsService struct {
	srv *sheets.Service
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