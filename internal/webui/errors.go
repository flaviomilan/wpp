package webui

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"wpp/internal/archive"
	"wpp/internal/localization"
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
		problemMessage(w, 500, "backend.storage_failure_check_the_service_status")
	case errors.Is(err, sql.ErrNoRows):
		problemMessage(w, 404, "backend.record_not_found_in_the_archive")
	case errors.Is(err, service.ErrReadOnly):
		problemMessage(w, 409, "backend.another_capture_process_controls_this_directory_stop_it_and_reopen")
	case errors.As(err, &validation):
		if validation.Code != "" {
			problemMessage(w, 400, validation.Code)
		} else {
			problem(w, 400, validation.Message)
		}
	case errors.As(err, &application):
		status := 400
		if application.Conflict {
			status = 409
		}
		if application.Code != "" {
			problemMessage(w, status, application.Code)
		} else {
			problem(w, status, application.Message)
		}
	default:
		problemMessage(w, 500, "backend.could_not_perform_the_operation_check_the_service_status")
	}
}

func problemMessage(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": localization.Text(localization.English, code, nil), "code": code})
}

func problemError(w http.ResponseWriter, status int, err error) {
	var message *localization.MessageError
	if errors.As(err, &message) {
		problemMessage(w, status, message.Key)
		return
	}
	problem(w, status, err.Error())
}
