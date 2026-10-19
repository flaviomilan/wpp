package service

import (
	"errors"
	"wpp/internal/archive"
)

type ApplicationError struct {
	Message  string
	Conflict bool
}

func (e *ApplicationError) Error() string { return e.Message }
func invalid(message string) error        { return &ApplicationError{Message: message} }
func conflict(message string) error       { return &ApplicationError{Message: message, Conflict: true} }

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
	c.errorText = "Falha de armazenamento. Confira espaço e permissões antes de retomar a captura."
	c.history.pause()
}
func persistenceFailure(err error) bool {
	var persistence *archive.PersistenceError
	return errors.As(err, &persistence)
}

func operationError(err error) string {
	var application *ApplicationError
	if errors.As(err, &application) {
		return application.Message
	}
	if persistenceFailure(err) {
		return "Falha de armazenamento. Confira espaço e permissões antes de retomar."
	}
	return "A execução falhou. Confira a conexão, os ajustes e tente novamente."
}
