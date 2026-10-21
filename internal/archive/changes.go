package archive

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"wpp/internal/localization"
)

func newSourceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "S-" + hex.EncodeToString(b[:])
}

func (s *Store) migrateAnalysis() error {
	return s.transaction(func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version > 7 {
			return localization.New("archive.database_version_is_newer_than_this_program")
		}
		if version >= 5 {
			return nil
		}
		if version == 4 {
			return migrateControl(tx)
		}
		if version == 3 {
			if err := migrateAudio(tx); err != nil {
				return err
			}
			return migrateControl(tx)
		}
		if version == 2 {
			if err := migratePeople(tx); err != nil {
				return err
			}
			if err := migrateAudio(tx); err != nil {
				return err
			}
			return migrateControl(tx)
		}
		rows, err := tx.Query("PRAGMA table_info(messages)")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure below reports errors before schema writes.
		hasSource := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, kind string
			var def any
			if err = rows.Scan(&cid, &name, &kind, &notNull, &def, &pk); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if name == "source_id" {
				hasSource = true
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		if !hasSource {
			if _, err = tx.Exec("ALTER TABLE messages ADD COLUMN source_id TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
		statements := []string{
			"CREATE TABLE IF NOT EXISTS source_redirects(old_id TEXT PRIMARY KEY,new_id TEXT NOT NULL)",
			"CREATE TABLE IF NOT EXISTS changes(seq INTEGER PRIMARY KEY AUTOINCREMENT,chat TEXT NOT NULL,source_id TEXT NOT NULL,revision INTEGER NOT NULL,operation TEXT NOT NULL,data BLOB NOT NULL,UNIQUE(source_id,revision))",
			"CREATE INDEX IF NOT EXISTS changes_chat_seq ON changes(chat,seq)",
			"CREATE TABLE IF NOT EXISTS analysis_state(chat TEXT PRIMARY KEY,next_seq INTEGER NOT NULL DEFAULT 0,fragment INTEGER NOT NULL DEFAULT 0,next_batch INTEGER NOT NULL DEFAULT 1,pending TEXT)",
			"CREATE TABLE IF NOT EXISTS analysis_batches(chat TEXT NOT NULL,id TEXT NOT NULL,state TEXT NOT NULL,next_seq INTEGER NOT NULL,fragment INTEGER NOT NULL,entry BLOB NOT NULL,manifest BLOB NOT NULL,PRIMARY KEY(chat,id))",
		}
		for _, statement := range statements {
			if _, err = tx.Exec(statement); err != nil {
				return err
			}
		}
		rows, err = tx.Query("SELECT rowid,data,source_id FROM messages ORDER BY ts,id,sender,from_me,chat")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure below reports errors before migration writes.
		type baseline struct {
			row    int64
			raw    []byte
			source string
		}
		var all []baseline
		for rows.Next() {
			var b baseline
			if err = rows.Scan(&b.row, &b.raw, &b.source); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			all = append(all, b)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for _, b := range all {
			if b.source == "" {
				b.source = newSourceID()
			}
			if _, err = tx.Exec("UPDATE messages SET source_id=? WHERE rowid=?", b.source, b.row); err != nil {
				return err
			}
			var m Message
			if err = json.Unmarshal(b.raw, &m); err != nil {
				return err
			}
			if err = recordChange(tx, b.source, m, b.raw); err != nil {
				return err
			}
		}
		_, err = tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS message_source ON messages(source_id) WHERE source_id<>''")
		if err != nil {
			return err
		}
		if err := migratePeople(tx); err != nil {
			return err
		}
		if err := migrateAudio(tx); err != nil {
			return err
		}
		return migrateControl(tx)
	})
}

func recordChange(tx *sql.Tx, source string, m Message, raw []byte) error {
	var revision int
	var previous []byte
	err := tx.QueryRow("SELECT revision,data FROM changes WHERE source_id=? ORDER BY revision DESC LIMIT 1", source).Scan(&revision, &previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if string(previous) == string(raw) {
		return nil
	}
	operation := "insert"
	if revision > 0 {
		operation = "update"
	}
	if m.Deleted {
		operation = "delete"
	} else if m.Edited {
		operation = "edit"
	} else if revision > 0 {
		var old Message
		if err = json.Unmarshal(previous, &old); err != nil {
			return err
		}
		if recoveredContent(old, m) {
			operation = "recover"
		}
	}
	if err = recordMessageEvents(tx, source, m, previous, revision+1); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO changes(chat,source_id,revision,operation,data) VALUES(?,?,?,?,?)", m.Chat, source, revision+1, operation, raw)
	if err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='event_migration'").Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		account, e := eventAccount(tx)
		if e != nil {
			return e
		}
		if account != "" {
			_, err = tx.Exec("INSERT INTO event_migration VALUES(?,?) ON CONFLICT(source_id) DO UPDATE SET revision=excluded.revision", source, revision+1)
		}
	}
	return err
}

type Change struct {
	Seq       int64
	Source    string
	Revision  int
	Operation string
	Message   Message
}

func scanChanges(rows *sql.Rows) ([]Change, error) {
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	var out []Change
	for rows.Next() {
		var c Change
		var raw []byte
		if err := rows.Scan(&c.Seq, &c.Source, &c.Revision, &c.Operation, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &c.Message); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func resolveSource(tx *sql.Tx, source string) (string, error) {
	seen := map[string]bool{}
	for {
		if seen[source] {
			return "", localization.New("archive.circular_evidence_redirect")
		}
		seen[source] = true
		var next string
		err := tx.QueryRow("SELECT new_id FROM source_redirects WHERE old_id=?", source).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return source, nil
		}
		if err != nil {
			return "", err
		}
		source = next
	}
}
