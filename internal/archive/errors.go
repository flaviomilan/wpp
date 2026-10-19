package archive

// ValidationError identifies invalid input without exposing storage errors.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalidInput(message string) error  { return &ValidationError{Message: message} }
