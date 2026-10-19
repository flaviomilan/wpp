package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Operation struct {
	ID        string    `json:"id"`
	Source    string    `json:"source_id,omitempty"`
	Kind      string    `json:"kind"`
	Chat      string    `json:"chat,omitempty"`
	State     string    `json:"state"`
	Phase     string    `json:"phase"`
	Detail    string    `json:"detail"`
	Started   time.Time `json:"started"`
	Updated   time.Time `json:"updated"`
	Total     int       `json:"total"`
	Completed int       `json:"completed"`
}

func migrateControl(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS service_settings(id INTEGER PRIMARY KEY CHECK(id=1),data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS service_operations(id TEXT PRIMARY KEY,data BLOB NOT NULL,updated INTEGER NOT NULL);

 PRAGMA user_version=5;`)
	if err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA table_info(audio_jobs)")
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
	exists := false
	for rows.Next() {
		var n, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&n, &name, &typ, &notnull, &def, &pk); err != nil {
			closeErr := rows.Close()
			return errors.Join(err, closeErr)
		}
		exists = exists || name == "phase"
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if !exists {
		_, err = tx.Exec("ALTER TABLE audio_jobs ADD COLUMN phase TEXT NOT NULL DEFAULT ''")
	}
	return err
}
func (s *Store) ControlSettings(initial []byte) ([]byte, error) {
	var raw []byte
	err := s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT OR IGNORE INTO service_settings(id,data) VALUES(1,?)", initial); e != nil {
			return e
		}
		return tx.QueryRow("SELECT data FROM service_settings WHERE id=1").Scan(&raw)
	})
	return raw, err
}
func (s *Store) SaveControlSettings(raw []byte) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, e := tx.Exec("UPDATE service_settings SET data=? WHERE id=1", raw)
		return e
	})
}
func (s *Store) SaveOperation(op Operation) error {
	raw, e := json.Marshal(op)
	if e != nil {
		return e
	}
	return s.transaction(func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO service_operations(id,data,updated) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data,updated=excluded.updated", op.ID, raw, op.Updated.UnixNano())
		return e
	})
}
func (s *Store) Operations() ([]Operation, error) {
	result := []Operation{}
	err := s.transaction(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT data FROM service_operations ORDER BY updated DESC LIMIT 30")
		if e != nil {
			return e
		}
		// Rows.Err reports iteration and automatic-close failures.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw []byte
			var op Operation
			if e = rows.Scan(&raw); e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &op); e != nil {
				return e
			}
			result = append(result, op)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) InterruptOperations() error {
	return s.transaction(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT data FROM service_operations")
		if e != nil {
			return e
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		var updates []Operation
		for rows.Next() {
			var raw []byte
			var op Operation
			if e = rows.Scan(&raw); e != nil {
				closeErr := rows.Close()
				return errors.Join(e, closeErr)
			}
			if e = json.Unmarshal(raw, &op); e != nil {
				closeErr := rows.Close()
				return errors.Join(e, closeErr)
			}
			if op.State == "queued" || op.State == "running" {
				op.State = "interrupted"
				op.Detail = "Execução interrompida. Solicite novamente para continuar."
				op.Updated = time.Now().UTC()
				updates = append(updates, op)
			}
		}
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		for _, op := range updates {
			raw, e := json.Marshal(op)
			if e != nil {
				return e
			}
			if _, e = tx.Exec("UPDATE service_operations SET data=?,updated=? WHERE id=?", raw, op.Updated.UnixNano(), op.ID); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Store) SetAudioPhase(job AudioJob, phase string) error {
	return s.transaction(func(tx *sql.Tx) error {
		source, e := resolveSource(tx, job.Source)
		if e != nil {
			return e
		}
		_, e = tx.Exec("UPDATE audio_jobs SET phase=? WHERE source_id=? AND token=? AND state='processing'", phase, source, job.Token)
		return e
	})
}
func (s *Store) AudioSnapshot(chat string) ([]string, error) {
	ids := []string{}
	err := s.transaction(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT source_id FROM audio_jobs WHERE chat=? AND (state='processing' OR (state='pending' AND metadata IS NOT NULL AND next_attempt<=?)) ORDER BY updated,source_id", chat, time.Now().Unix())
		if e != nil {
			return e
		}
		// Rows.Err reports iteration and automatic-close failures.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}
func (s *Store) AudioState(source string) (string, error) {
	var state string
	err := s.transaction(func(tx *sql.Tx) error {
		id, e := resolveSource(tx, source)
		if e != nil {
			return e
		}
		return tx.QueryRow("SELECT state FROM audio_jobs WHERE source_id=?", id).Scan(&state)
	})
	return state, err
}
