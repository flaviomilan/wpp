package service

import (
	"errors"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type ApplicationError struct {
	Code     string `json:"code,omitempty"`
	Message  string
	Conflict bool
}

func (e *ApplicationError) Error() string { return e.Message }
func invalid(message string) error {
	return &ApplicationError{Message: localMessage(message), Code: localization.CodeForKey(message)}
}
func conflict(message string) error {
	return &ApplicationError{Message: localMessage(message), Code: localization.CodeForKey(message), Conflict: true}
}

func (c *Controller) storageFailure(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.storageFailureLocked(err)
	c.mu.Unlock()
}

func (c *Controller) storageFailureLocked(err error) {
	c.blocked = true
	c.storageError = err
	c.connection = "error"
	c.errorText = "backend.storage_failure_check_disk_space_and_permissions_before_resuming_capture"
	c.history.pause()
}
func persistenceFailure(err error) bool {
	var persistence *archive.PersistenceError
	return errors.As(err, &persistence)
}

func operationError(err error) string {
	var application *ApplicationError
	if errors.As(err, &application) {
		if application.Code != "" {
			return application.Code
		}
		return application.Message
	}
	if persistenceFailure(err) {
		return "backend.storage_failure_check_disk_space_and_permissions_before_resuming"
	}
	return "backend.the_operation_failed_check_the_connection_and_settings_then_try"
}

func localMessage(key string) string {
	if localization.Known(key) {
		return localization.Text(localization.English, key, nil)
	}
	return key
}

func (e *ApplicationError) MessageCode() string { return e.Code }
