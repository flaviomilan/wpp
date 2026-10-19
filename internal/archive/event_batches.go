package archive

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// EventBatchService publishes immutable reservations; consumers acknowledge only after committing.
type EventBatchService struct {
	Store *Store
	Dir   string
}
type EventManifest struct {
	SchemaVersion string            `json:"schema_version"`
	AccountID     string            `json:"account_id"`
	GroupID       string            `json:"group_id"`
	BatchID       string            `json:"batch_id"`
	FromCursor    int64             `json:"from_cursor"`
	ToCursor      int64             `json:"to_cursor"`
	Count         int               `json:"count"`
	FileSHA256    map[string]string `json:"file_sha256"`
}

func (b EventBatchService) root(group, id string) string {
	return filepath.Join(b.Dir, "events", "chat-"+digest([]byte(group)), id)
}
func (b EventBatchService) Next(group string) (string, error) {
	if err := validateGroup(group); err != nil {
		return "", err
	}
	if err := b.Store.migrateEvents(); err != nil {
		return "", err
	}
	var entry, manifest []byte
	var id string
	err := b.Store.transaction(func(tx *sql.Tx) error {
		if err := requireEventAccount(tx); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO event_state(chat) VALUES(?)", group); err != nil {
			return err
		}
		var cursor int64
		var pending sql.NullString
		var number int
		if err := tx.QueryRow("SELECT cursor,pending,next_batch FROM event_state WHERE chat=?", group).Scan(&cursor, &pending, &number); err != nil {
			return err
		}
		if pending.Valid {
			id = pending.String
			return tx.QueryRow("SELECT entry,manifest FROM event_batches WHERE chat=? AND id=?", group, id).Scan(&entry, &manifest)
		}
		rows, err := tx.Query("SELECT seq,data FROM events WHERE chat=? AND seq>? ORDER BY seq LIMIT 100", group, cursor)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit close reports errors before reserving the batch.
		to := cursor
		count := 0
		for rows.Next() {
			var seq int64
			var raw []byte
			if err = rows.Scan(&seq, &raw); err != nil {
				break
			}
			if count > 0 && len(entry)+len(raw)+1 > 1024*1024 {
				break
			}
			entry = append(entry, raw...)
			entry = append(entry, '\n')
			to = seq
			count++
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		account, err := eventAccount(tx)
		if err != nil {
			return err
		}
		id = fmt.Sprintf("E%06d", number)
		manifest, err = json.MarshalIndent(EventManifest{SchemaVersion: "1.0", AccountID: account, GroupID: group, BatchID: id, FromCursor: cursor, ToCursor: to, Count: count, FileSHA256: map[string]string{"events.jsonl": digest(entry)}}, "", "  ")
		if err != nil {
			return err
		}
		manifest = append(manifest, '\n')
		if _, err = tx.Exec("INSERT INTO event_batches VALUES(?,?,'pending',?,?,?)", group, id, to, entry, manifest); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE event_state SET pending=?,next_batch=next_batch+1 WHERE chat=?", id, group)
		return err
	})
	if err != nil || id == "" {
		return "", err
	}
	root := b.root(group, id)
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	if err = AtomicWrite(filepath.Join(root, "events.jsonl"), entry); err != nil {
		return "", err
	}
	if err = AtomicWrite(filepath.Join(root, "manifest.json"), manifest); err != nil {
		return "", err
	}
	return filepath.Join(root, "events.jsonl"), nil
}
func (b EventBatchService) Ack(group, id string) error {
	if err := validateGroup(group); err != nil {
		return err
	}
	return b.Store.transaction(func(tx *sql.Tx) error {
		var state string
		var to int64
		var entry, manifest []byte
		if err := tx.QueryRow("SELECT state,to_cursor,entry,manifest FROM event_batches WHERE chat=? AND id=?", group, id).Scan(&state, &to, &entry, &manifest); err != nil {
			return err
		}
		if state == "acked" {
			return nil
		}
		var pending sql.NullString
		if err := tx.QueryRow("SELECT pending FROM event_state WHERE chat=?", group).Scan(&pending); err != nil {
			return err
		}
		if !pending.Valid || pending.String != id {
			return errors.New("lote não é o pendente esperado")
		}
		// Validate against reserved bytes, not a caller-controlled manifest. Paths use the trusted stored ID.
		for name, want := range map[string][]byte{"events.jsonl": entry, "manifest.json": manifest} {
			got, err := os.ReadFile(filepath.Join(b.root(group, id), name))
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				return errors.New("lote alterado; execute events next para restaurar")
			}
		}
		if _, err := tx.Exec("UPDATE event_batches SET state='acked' WHERE chat=? AND id=?", group, id); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE event_state SET cursor=?,pending=NULL WHERE chat=?", to, group)
		return err
	})
}
