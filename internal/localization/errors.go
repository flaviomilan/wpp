package localization

import (
	"errors"
	"fmt"
	"strings"
)

// MessageError preserves a stable message identity and any wrapped error cause.
type MessageError struct {
	Key       string
	Arguments []any
	cause     error
}

func (e *MessageError) Error() string {
	if !Known(e.Key) {
		return Text(English, "error.generic", nil)
	}
	return fmt.Errorf(Text(English, e.Key, nil), e.Arguments...).Error()
}
func (e *MessageError) Unwrap() error { return e.cause }
func New(key string) error            { return &MessageError{Key: key} }
func Errorf(key string, args ...any) error {
	cause := fmt.Errorf(Text(English, key, nil), args...)
	return &MessageError{Key: key, Arguments: args, cause: cause}
}

// LocalizedError changes presentation while retaining error identity for callers.
type LocalizedError struct {
	Language string
	Err      error
}

func (e *LocalizedError) Error() string { return Render(e.Err, e.Language) }
func (e *LocalizedError) Unwrap() error { return e.Err }
func InLanguage(err error, language string) error {
	if err == nil {
		return nil
	}
	return &LocalizedError{Language: Normalize(language), Err: err}
}
func Render(err error, language string) string {
	if err == nil {
		return ""
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := []string{}
		for _, child := range joined.Unwrap() {
			if child != nil {
				parts = append(parts, Render(child, language))
			}
		}
		return strings.Join(parts, "\n")
	}
	var message *MessageError
	if errors.As(err, &message) {
		if !Known(message.Key) {
			return Text(English, "error.generic", nil)
		}
		return fmt.Errorf(Text(language, message.Key, nil), message.Arguments...).Error()
	}
	if coded, ok := err.(interface{ MessageCode() string }); ok && Known(coded.MessageCode()) {
		return Text(language, coded.MessageCode(), nil)
	}
	return err.Error()
}

func (e *MessageError) MessageCode() string { return e.Key }
