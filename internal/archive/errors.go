package archive

import "wpp/internal/localization"

// ValidationError identifies invalid input without exposing storage errors.
type ValidationError struct {
	Message string
	Code    string
}

func (e *ValidationError) Error() string { return e.Message }
func invalidInput(message string) error {
	if localization.Known(message) {
		return &ValidationError{Message: localization.Text(localization.English, message, nil), Code: message}
	}
	return &ValidationError{Message: message}
}

func (e *ValidationError) MessageCode() string { return e.Code }
