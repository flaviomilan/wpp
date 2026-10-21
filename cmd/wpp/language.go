package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

// invocationLanguage resolves only global options, stopping at the command.
// It never creates a directory, database, session, or processing controller.
func invocationLanguage(args []string) (language string, err error) {
	directory, override := "./data", ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			break
		}
		name, value, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name == "h" || name == "help" {
			continue
		}
		if !inline && i+1 < len(args) {
			i++
			value = args[i]
		}
		if name == "data-dir" {
			directory = value
		}
		if name == "language" {
			override = value
			if !localization.Valid(value) {
				return localization.English, errors.New(localization.Text(localization.English, "language.invalid", nil))
			}
		}
	}
	if override != "" {
		return override, nil
	}
	if _, err = os.Stat(filepath.Join(directory, "archive.db")); errors.Is(err, os.ErrNotExist) {
		return localization.English, nil
	}
	if err != nil {
		return localization.English, err
	}
	return archive.ReadApplicationLanguage(directory)
}
