package archive

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"wpp/internal/localization"
)

// writeJSONL holds a consistent read-only WAL snapshot while capture continues.
// It exports the immutable event journal, including edits, deletions and transcriptions.
func (s *Store) writeJSONL(ctx context.Context, output io.Writer, id, source string) (chat Chat, err error) {
	reader, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: s.path}).String()+"?mode=ro&_txlock=deferred&_busy_timeout=5000")
	if err != nil {
		return chat, err
	}
	reader.SetMaxOpenConns(1)
	defer func() { err = errors.Join(err, reader.Close()) }()
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return chat, err
	}
	defer func() { _ = tx.Rollback() }()
	id, err = canonicalContext(ctx, tx, id)
	if err != nil {
		return chat, err
	}
	err = tx.QueryRowContext(ctx, "SELECT id,name,tracked,file,generation,exported,(SELECT COUNT(*) FROM messages WHERE chat=chats.id) FROM chats WHERE id=?", id).Scan(&chat.ID, &chat.Name, &chat.Tracked, &chat.File, &chat.Generation, &chat.Exported, &chat.Count)
	if err != nil {
		return chat, err
	}
	if chat.Count > 0 {
		if err = requireEventAccount(tx); err != nil {
			return chat, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT chat,data FROM events WHERE (chat=? OR chat IN (SELECT alias FROM aliases WHERE canonical=?)) AND (?='' OR json_extract(data,'$.payload.source_id')=?) ORDER BY seq", id, id, source, source)
	if err != nil {
		return chat, err
	}
	defer func() { _ = rows.Close() }()
	writer := exportWriter{writer: output, ctx: ctx}
	count := 0
	for rows.Next() {
		var raw []byte
		var eventChat string
		if err = rows.Scan(&eventChat, &raw); err != nil {
			return chat, err
		}
		var event Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return chat, err
		}
		if !utf8.Valid(raw) || event.SchemaVersion != "1.0" || event.EventID == "" || event.Sequence < 1 || event.AccountID == "" || event.GroupID != eventChat || event.Source != "whatsapp" || event.EventType == "" || event.Payload == nil {
			return chat, localization.New("archive.invalid_archived_event")
		}
		var compact bytes.Buffer
		if err = json.Compact(&compact, raw); err != nil {
			return chat, err
		}
		if _, err = writer.Write(compact.Bytes()); err != nil {
			return chat, err
		}
		if _, err = writer.WriteString("\n"); err != nil {
			return chat, err
		}
		count++
	}
	if err = errors.Join(rows.Err(), rows.Close(), ctx.Err()); err != nil {
		return chat, err
	}
	if source != "" && count == 0 {
		return chat, invalidInput("archive.evidence_not_found_in_this_group")
	}
	return chat, tx.Commit()
}
func validateGroup(group string) error {
	if !strings.HasSuffix(group, "@g.us") || strings.ContainsAny(group, "/\\\x00") {
		return invalidInput("archive.use_a_group_identifier_ending_in_g_us")
	}
	return nil
}

// Evidence exports all observed events of the requested internal source as JSONL.
func (b EventBatchService) Evidence(group, source string) (string, error) {
	if err := validateGroup(group); err != nil {
		return "", err
	}
	if source == "" {
		return "", invalidInput("archive.specify_source")
	}
	if err := b.Store.migrateEvents(); err != nil {
		return "", err
	}
	path := filepath.Join(b.Dir, "events", "chat-"+digest([]byte(group)), "evidence", "source-"+digest([]byte(source))+".jsonl")
	err := atomicWriteStream(path, func(w io.Writer) error {
		_, err := b.Store.writeJSONL(context.Background(), w, group, source)
		return err
	})
	if err != nil {
		return "", err
	}
	return path, nil
}
