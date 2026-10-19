package webui

import (
	"database/sql"
	"errors"
	"net/http"
	"wpp/internal/archive"
	"wpp/internal/service"
)

// All application failures cross the HTTP interface through this classification.
// Unknown causes are intentionally not exposed to the browser.
func respondError(w http.ResponseWriter, err error) {
	var storage *archive.PersistenceError
	var application *service.ApplicationError
	var validation *archive.ValidationError
	switch {
	case errors.As(err, &storage):
		problem(w, 500, "Falha de armazenamento. Confira o estado do serviço.")
	case errors.Is(err, sql.ErrNoRows):
		problem(w, 404, "Registro não encontrado no arquivo.")
	case errors.Is(err, service.ErrReadOnly):
		problem(w, 409, service.ErrReadOnly.Error())
	case errors.As(err, &validation):
		problem(w, 400, validation.Message)
	case errors.As(err, &application):
		status := 400
		if application.Conflict {
			status = 409
		}
		problem(w, status, application.Message)
	default:
		problem(w, 500, "Não foi possível executar a operação. Confira o estado do serviço.")
	}
}
