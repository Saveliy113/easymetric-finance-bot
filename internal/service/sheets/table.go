package sheets

import (
	"context"
	"errors"
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
