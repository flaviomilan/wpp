package archive

import (
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"wpp/internal/localization"
)

// ApplicationLanguage reads presentation metadata without creating a default row.
func (s *Store) ApplicationLanguage() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return readApplicationLanguage(s.db)
}

func readApplicationLanguage(db *sql.DB) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM meta WHERE key='application_language'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return localization.English, nil
	}
	if err != nil {
		return "", err
	}
	return localization.Normalize(value), nil
}

// ReadApplicationLanguage opens an existing archive read-only for CLI bootstrap.
// The caller checks existence first; mode=ro never creates a missing database.
func ReadApplicationLanguage(dir string) (language string, resultErr error) {
	path, err := filepath.Abs(filepath.Join(dir, "archive.db"))
	if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	return readApplicationLanguage(db)
}

// SaveApplicationLanguage changes only presentation metadata, independently of capture ownership.
func (s *Store) SaveApplicationLanguage(language string) error {
	if !localization.Valid(language) {
		return &ValidationError{Message: localization.Text(localization.English, "language.invalid", nil)}
	}
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO meta(key,value) VALUES('application_language',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", language)
		return err
	})
}
