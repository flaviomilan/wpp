package archive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Join(dir, "archive.db"))
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: abs}).String()+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: abs}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chats (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', tracked INTEGER NOT NULL DEFAULT 0, file TEXT NOT NULL, generation INTEGER NOT NULL DEFAULT 0, exported INTEGER NOT NULL DEFAULT -1, seed BLOB);
CREATE TABLE IF NOT EXISTS aliases (alias TEXT PRIMARY KEY, canonical TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS messages (chat TEXT NOT NULL REFERENCES chats(id), id TEXT NOT NULL, sender TEXT NOT NULL, from_me INTEGER NOT NULL, ts INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(chat,id,sender,from_me));
CREATE INDEX IF NOT EXISTS message_order ON messages(chat,ts,id,sender,from_me);
CREATE TABLE IF NOT EXISTS attempts (id INTEGER PRIMARY KEY AUTOINCREMENT, chat TEXT NOT NULL REFERENCES chats(id), started INTEGER NOT NULL, finished INTEGER NOT NULL DEFAULT 0, anchor TEXT NOT NULL, anchor_key TEXT NOT NULL DEFAULT '', state TEXT NOT NULL, detail TEXT NOT NULL, boundary_observed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS retired_exports (file TEXT PRIMARY KEY, chat TEXT NOT NULL);
`)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err = s.migrateAnalysis(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err = s.migrateEvents(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err = s.repairLegacyReactions(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err = os.Chmod(abs, 0600); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) transaction(fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Rollback after Commit returns ErrTxDone; on failure the original error is preserved.
	defer func() { _ = tx.Rollback() }()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func canonical(tx *sql.Tx, id string) (string, error) {
	return canonicalContext(context.Background(), tx, id)
}
func canonicalContext(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, "SELECT canonical FROM aliases WHERE alias=?", id).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return id, nil
	}
	return value, err
}
func ensureChat(tx *sql.Tx, id, name string) error {
	hash := sha256.Sum256([]byte(id))
	file := "chat-" + hex.EncodeToString(hash[:]) + ".jsonl"
	_, err := tx.Exec(`INSERT INTO chats(id,name,file) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE chats.name END,generation=chats.generation+CASE WHEN excluded.name<>'' AND excluded.name<>chats.name THEN 1 ELSE 0 END`, id, name, file)
	return err
}

func (s *Store) BindAccount(id string) error {
	return s.transaction(func(tx *sql.Tx) error {
		var old string
		err := tx.QueryRow("SELECT value FROM meta WHERE key='account'").Scan(&old)
		if err == nil && old != id {
			return errors.New("o diretório pertence a outra conta; use --data-dir diferente")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.Exec("INSERT OR IGNORE INTO meta(key,value) VALUES('account',?)", id)
		return err
	})
}

func (s *Store) Discover(id, name string) error {
	return s.transaction(func(tx *sql.Tx) error {
		id, err := canonical(tx, id)
		if err != nil {
			return err
		}
		return ensureChat(tx, id, name)
	})
}

func (s *Store) Track(id string, enabled bool) error {
	return s.transaction(func(tx *sql.Tx) error {
		id, err := canonical(tx, id)
		if err != nil {
			return err
		}
		if err = ensureChat(tx, id, ""); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE chats SET tracked=?,generation=generation+1 WHERE id=?", enabled, id)
		if err != nil {
			return err
		}
		if enabled {
			var seed []byte
			if err = tx.QueryRow("SELECT seed FROM chats WHERE id=?", id).Scan(&seed); err != nil {
				return err
			}
			if len(seed) > 0 {
				var m Message
				if err = json.Unmarshal(seed, &m); err != nil {
					return err
				}
				_, err = putMessage(tx, m)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func keySender(m Message) string {
	if !m.Group {
		return ""
	}
	return m.Sender
}

// merge makes replay idempotent and prevents historical originals from
// resurrecting deleted messages or overwriting newer edits.
func merge(old, next Message) (merged Message) {
	defer func() {
		if next.Evidence != nil && (next.Version > old.Version || (next.Deleted && !old.Deleted) || recoveredContent(old, next)) {
			merged.Evidence = next.Evidence
		}
		if len(next.AudioMetadata) > 0 {
			merged.AudioMetadata = next.AudioMetadata
		}
		if merged.Deleted || merged.Kind != "áudio" {
			merged.Transcript = nil
		} else if merged.Transcript == nil {
			merged.Transcript = old.Transcript
			if merged.Transcript == nil {
				merged.Transcript = next.Transcript
			}
		}
	}()
	if next.Deleted && !next.Mutation {
		old.Deleted = true
		old.Text = ""
		old.FileName = ""
		old.ReplyID = ""
		old.ReplySender = ""
		return old
	}
	if old.Mutation && !next.Mutation {
		base := next
		if old.Deleted {
			base.Deleted = true
			base.Text = ""
			base.FileName = ""
			base.ReplyID = ""
			base.ReplySender = ""
		}
		if old.Edited && !old.Deleted {
			base.Text = old.Text
			base.Kind = old.Kind
			base.FileName = old.FileName
			base.Edited = true
		}
		base.Version = old.Version
		return base
	}
	if next.Mutation {
		if next.Deleted {
			old.Deleted = true
			old.Text = ""
			old.FileName = ""
			old.ReplyID = ""
			old.ReplySender = ""
		}
		if !old.Deleted && next.Edited && next.Version >= old.Version {
			old.Text = next.Text
			old.Kind = next.Kind
			old.FileName = next.FileName
			old.Edited = true
		}
		if next.Version > old.Version {
			old.Version = next.Version
		}
		return old
	}
	if old.Deleted || old.Edited {
		return old
	}
	if legacyReaction(old) && next.Kind == "reação" {
		return next
	}
	if recoveredContent(old, next) {
		return next
	}
	if next.Edited && next.Version >= old.Version {
		return next
	}
	if old.Evidence == nil && next.Evidence != nil {
		old.Evidence = next.Evidence
	}
	if old.Author == "" && next.Author != "" {
		old.Author = next.Author
	}
	return old
}

func recoveredContent(old, next Message) bool {
	if old.Kind == "reação" && old.Text == "Reação indisponível" {
		return next.Kind == "reação" && (next.Text != "Reação indisponível" || (old.ReplyID == "" && next.ReplyID != ""))
	}
	unavailable := old.Kind == "indisponível" || strings.HasPrefix(old.Kind, "não interpretado")
	return unavailable && next.Kind != "indisponível" && !strings.HasPrefix(next.Kind, "não interpretado") && (next.Kind != "reação" || next.Text != "Reação indisponível")
}

func putMessage(tx *sql.Tx, m Message) (bool, error) {
	return putMessageSource(tx, m, "")
}
func putMessageSource(tx *sql.Tx, m Message, preferredSource string) (bool, error) {
	metadata := m.AudioMetadata
	if m.Evidence != nil {
		if err := tx.QueryRow("SELECT name FROM chats WHERE id=?", m.Chat).Scan(&m.Evidence.GroupName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	if m.ID == "" || m.Chat == "" {
		return false, errors.New("mensagem sem chave")
	}
	var err error
	m.Chat, err = canonical(tx, m.Chat)
	if err != nil {
		return false, err
	}
	m.Sender, err = canonical(tx, m.Sender)
	if err != nil {
		return false, err
	}
	m.ReplySender, err = canonical(tx, m.ReplySender)
	if err != nil {
		return false, err
	}
	if err = ensureChat(tx, m.Chat, ""); err != nil {
		return false, err
	}
	if m.Group {
		if err = observePerson(tx, m.Chat, m.Sender, m.Author); err != nil {
			return false, err
		}
		if err = observePerson(tx, m.Chat, m.ReplySender, ""); err != nil {
			return false, err
		}
	}
	var raw []byte
	var source string
	err = tx.QueryRow("SELECT data,source_id FROM messages WHERE chat=? AND id=? AND sender=? AND from_me=?", m.Chat, m.ID, keySender(m), m.FromMe).Scan(&raw, &source)
	inserted := errors.Is(err, sql.ErrNoRows)
	if err != nil && !inserted {
		return false, err
	}
	snapshotObservation := m
	if !inserted {
		var old Message
		if err = json.Unmarshal(raw, &old); err != nil {
			return false, err
		}
		m = merge(old, m)
	}
	if source == "" {
		source = preferredSource
	}
	if source == "" {
		source = newSourceID()
	}
	if err = saveAudio(tx, source, preferredSource, m, metadata); err != nil {
		return false, err
	}
	if preferredSource != "" && preferredSource != source {
		if _, err = tx.Exec("INSERT INTO source_redirects(old_id,new_id) VALUES(?,?) ON CONFLICT(old_id) DO UPDATE SET new_id=excluded.new_id", preferredSource, source); err != nil {
			return false, err
		}
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	if string(encoded) == string(raw) {
		return inserted, recordReactionSnapshots(tx, snapshotObservation)
	}
	_, err = tx.Exec(`INSERT INTO messages(chat,id,sender,from_me,ts,data,source_id) VALUES(?,?,?,?,?,?,?) ON CONFLICT(chat,id,sender,from_me) DO UPDATE SET ts=excluded.ts,data=excluded.data`, m.Chat, m.ID, keySender(m), m.FromMe, m.Time.Unix(), encoded, source)
	if err != nil {
		return false, err
	}
	if err = recordChange(tx, source, m, encoded); err != nil {
		return false, err
	}
	if err = recordReactionSnapshots(tx, snapshotObservation); err != nil {
		return false, err
	}
	_, err = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=?", m.Chat)
	return inserted, err
}

// Ingest also caches the newest envelope for discovery. An untracked chat does
// not acquire an archive; the single cached envelope seeds its first scan.
func (s *Store) Ingest(messages []Message) (BatchResult, error) {
	var result BatchResult
	err := s.transaction(func(tx *sql.Tx) error {
		for _, m := range messages {
			var err error
			m.Chat, err = canonical(tx, m.Chat)
			if err != nil {
				return err
			}
			m.Sender, err = canonical(tx, m.Sender)
			if err != nil {
				return err
			}
			if err = ensureChat(tx, m.Chat, ""); err != nil {
				return err
			}
			var tracked bool
			var seed []byte
			if err = tx.QueryRow("SELECT tracked,seed FROM chats WHERE id=?", m.Chat).Scan(&tracked, &seed); err != nil {
				return err
			}
			incoming := m
			if !m.Mutation {
				var old Message
				if len(seed) > 0 {
					if err = json.Unmarshal(seed, &old); err != nil {
						return err
					}
					old.Sender, err = canonical(tx, old.Sender)
					if err != nil {
						return err
					}
				}
				if len(seed) > 0 && sameKey(old, m) {
					m = merge(old, m)
				}
				if len(seed) == 0 || sameKey(old, m) || m.Time.After(old.Time) || (m.Time.Equal(old.Time) && m.ID > old.ID) {
					encoded, e := json.Marshal(m)
					if e != nil {
						return e
					}
					if _, err = tx.Exec("UPDATE chats SET seed=? WHERE id=?", encoded, m.Chat); err != nil {
						return err
					}
				}
			} else if len(seed) > 0 {
				var old Message
				if err = json.Unmarshal(seed, &old); err != nil {
					return err
				}
				old.Sender, err = canonical(tx, old.Sender)
				if err != nil {
					return err
				}
				if sameKey(old, m) {
					encoded, e := json.Marshal(merge(old, m))
					if e != nil {
						return e
					}
					if _, err = tx.Exec("UPDATE chats SET seed=? WHERE id=?", encoded, m.Chat); err != nil {
						return err
					}
				}
			}
			if !tracked {
				continue
			}
			inserted, e := putMessage(tx, incoming)
			if e != nil {
				return e
			}
			if inserted {
				result.Inserted++
			} else {
				result.Overlap++
			}
			if !m.Mutation && (result.Oldest == nil || m.Time.Before(result.Oldest.Time) || (m.Time.Equal(result.Oldest.Time) && m.ID < result.Oldest.ID)) {
				copy := m
				result.Oldest = &copy
			}
		}
		return nil
	})
	return result, err
}

// Alias collapses LID/phone identities, including messages that arrived before
// the mapping. Mapping and rekeying are committed in one transaction.
func (s *Store) Alias(alias, target string) error {
	if alias == "" || target == "" || alias == target {
		return nil
	}
	return s.transaction(func(tx *sql.Tx) error {
		old, err := canonical(tx, alias)
		if err != nil {
			return err
		}
		target, err = canonical(tx, target)
		if err != nil {
			return err
		}
		if old == target {
			_, err = tx.Exec("INSERT OR REPLACE INTO aliases VALUES(?,?)", alias, target)
			return err
		}
		movedName, e := tx.Exec("INSERT INTO people_names(id,name) SELECT ?,name FROM people_names WHERE id=? ON CONFLICT(id) DO NOTHING", target, old)
		if e != nil {
			return e
		}
		moved, e := movedName.RowsAffected()
		if e != nil {
			return e
		}
		if moved > 0 {
			if _, err = tx.Exec("UPDATE chats SET generation=generation+1"); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("DELETE FROM people_names WHERE id=?", old); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO group_people(chat,person,detected_name) SELECT chat,?,detected_name FROM group_people WHERE person=? ON CONFLICT(chat,person) DO UPDATE SET detected_name=CASE WHEN group_people.detected_name='' THEN excluded.detected_name ELSE group_people.detected_name END", target, old); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM group_people WHERE person=?", old); err != nil {
			return err
		}
		if err = ensureChat(tx, target, ""); err != nil {
			return err
		}
		var name, file string
		var tracked bool
		var seed []byte
		err = tx.QueryRow("SELECT name,file,tracked,seed FROM chats WHERE id=?", old).Scan(&name, &file, &tracked, &seed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			_, err = tx.Exec(`UPDATE chats SET name=CASE WHEN name='' THEN ? ELSE name END, file=CASE WHEN tracked=0 AND ? THEN ? ELSE file END, tracked=MAX(tracked,?), generation=generation+1 WHERE id=?`, name, tracked, file, tracked, target)
			if err != nil {
				return err
			}
			var finalFile string
			if err = tx.QueryRow("SELECT file FROM chats WHERE id=?", target).Scan(&finalFile); err != nil {
				return err
			}
			if file != finalFile {
				if _, err = tx.Exec("INSERT OR REPLACE INTO retired_exports(file,chat) VALUES(?,?)", file, target); err != nil {
					return err
				}
			}
			if len(seed) > 0 {
				var m Message
				if err = json.Unmarshal(seed, &m); err != nil {
					return err
				}
				m.Chat = target
				if m.Sender == old {
					m.Sender = target
				}
				encoded, e := json.Marshal(m)
				if e != nil {
					return e
				}
				var current []byte
				if err = tx.QueryRow("SELECT seed FROM chats WHERE id=?", target).Scan(&current); err != nil {
					return err
				}
				var c Message
				if len(current) > 0 {
					if err = json.Unmarshal(current, &c); err != nil {
						return err
					}
				}
				if len(current) == 0 || m.Time.After(c.Time) {
					if _, err = tx.Exec("UPDATE chats SET seed=? WHERE id=?", encoded, target); err != nil {
						return err
					}
				}
			}
		}
		rows, err := tx.Query("SELECT chat,id,sender,from_me,data,source_id FROM messages")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		type row struct {
			chat, id, sender string
			from             bool
			m                Message
			source           string
		}
		var affected []row
		for rows.Next() {
			var r row
			var raw []byte
			if err = rows.Scan(&r.chat, &r.id, &r.sender, &r.from, &raw, &r.source); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if err = json.Unmarshal(raw, &r.m); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if r.m.Chat == old || r.m.Sender == old || r.m.ReplySender == old {
				affected = append(affected, r)
			}
		}
		if err = rows.Err(); err != nil {
			closeErr := rows.Close()
			return errors.Join(err, closeErr)
		}
		_ = rows.Close() // Read-only cursor; the iteration or original error is checked.
		if _, err = tx.Exec("UPDATE aliases SET canonical=? WHERE canonical=?", target, old); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO aliases VALUES(?,?)", old, target); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO aliases VALUES(?,?)", alias, target); err != nil {
			return err
		}
		for _, r := range affected {
			if _, err = tx.Exec("DELETE FROM messages WHERE chat=? AND id=? AND sender=? AND from_me=?", r.chat, r.id, r.sender, r.from); err != nil {
				return err
			}
		}
		for _, r := range affected {
			if r.m.Group && r.m.Sender == old {
				oldKey := anchor(r.m.ID, keySender(r.m), r.m.FromMe)
				newKey := anchor(r.m.ID, target, r.m.FromMe)
				if _, err = tx.Exec("UPDATE attempts SET anchor_key=? WHERE chat=? AND anchor_key=?", newKey, r.chat, oldKey); err != nil {
					return err
				}
			}
			if _, err = putMessageSource(tx, r.m, r.source); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("UPDATE attempts SET chat=? WHERE chat=?", target, old); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE retired_exports SET chat=? WHERE chat=?", target, old); err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM chats WHERE id=?", old)
		return err
	})
}

func (s *Store) Canonical(id string) (string, error) {
	var out string
	err := s.transaction(func(tx *sql.Tx) error { var e error; out, e = canonical(tx, id); return e })
	return out, err
}

func (s *Store) Chats() ([]Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT c.id,c.name,c.tracked,c.file,c.generation,c.exported,c.seed,COUNT(m.id),COALESCE(MIN(CASE WHEN json_extract(m.data,'$.Mutation')=0 THEN m.ts END),0),COALESCE(MAX(CASE WHEN json_extract(m.data,'$.Mutation')=0 THEN m.ts END),0) FROM chats c LEFT JOIN messages m ON m.chat=c.id GROUP BY c.id ORDER BY c.name,c.id`)
	if err != nil {
		return nil, err
	}
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	var chats []Chat
	for rows.Next() {
		var c Chat
		var raw []byte
		var first, last int64
		if err = rows.Scan(&c.ID, &c.Name, &c.Tracked, &c.File, &c.Generation, &c.Exported, &raw, &c.Count, &first, &last); err != nil {
			return nil, err
		}
		if first != 0 {
			c.First = time.Unix(first, 0).UTC()
		}
		if last != 0 {
			c.Last = time.Unix(last, 0).UTC()
		}
		if len(raw) > 0 {
			var m Message
			if err = json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			c.Seed = &m
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

func readMessages(tx *sql.Tx, id string) ([]Message, error) {
	rows, err := tx.Query("SELECT data FROM messages WHERE chat=? ORDER BY ts,id,sender,from_me", id)
	if err != nil {
		return nil, err
	}
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var raw []byte
		var m Message
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Snapshot is a coherent database view. Export marks only this generation, so
// events arriving during rendering remain dirty for the following export.
func (s *Store) Snapshot(id string) (Chat, []Message, []Attempt, error) {
	var c Chat
	var messages []Message
	var attempts []Attempt
	err := s.transaction(func(tx *sql.Tx) error {
		id, e := canonical(tx, id)
		if e != nil {
			return e
		}
		if e = tx.QueryRow("SELECT id,name,tracked,file,generation,exported FROM chats WHERE id=?", id).Scan(&c.ID, &c.Name, &c.Tracked, &c.File, &c.Generation, &c.Exported); e != nil {
			return e
		}
		messages, e = readMessages(tx, id)
		if e != nil {
			return e
		}
		attempts, e = readAttempts(tx, id)
		return e
	})
	return c, messages, attempts, err
}
func (s *Store) MarkExported(id string, generation int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE chats SET exported=? WHERE id=?", generation, id)
		return err
	})
}

func (s *Store) BeginAttempt(chat, anchor, detail string) (int64, error) {
	return s.beginAttempt(chat, anchor, "", detail)
}
func (s *Store) BeginRecovery(chat string, checkpoint *Message, detail string) (int64, error) {
	id, key := "", ""
	if checkpoint != nil {
		id = checkpoint.ID
		key = anchor(checkpoint.ID, keySender(*checkpoint), checkpoint.FromMe)
	}
	return s.beginAttempt(chat, id, key, detail)
}
func (s *Store) beginAttempt(chat, anchor, key, detail string) (int64, error) {
	var id int64
	err := s.transaction(func(tx *sql.Tx) error {
		chat, e := canonical(tx, chat)
		if e != nil {
			return e
		}
		res, e := tx.Exec("INSERT INTO attempts(chat,started,anchor,anchor_key,state,detail) VALUES(?,?,?,?,'running',?)", chat, time.Now().Unix(), anchor, key, detail)
		if e != nil {
			return e
		}
		id, e = res.LastInsertId()
		if e != nil {
			return e
		}
		_, e = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=?", chat)
		return e
	})
	return id, err
}
func (s *Store) FinishAttempt(id int64, detail string) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE attempts SET finished=?,state='unconfirmed',detail=? WHERE id=?", time.Now().Unix(), detail, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=(SELECT chat FROM attempts WHERE id=?)", id)
		return err
	})
}

// Seeing a boundary only resolves the need to retry to that anchor; it is not
// a claim of transcript completeness. Keep attempts and their uncertainty.
func (s *Store) ObserveBoundary(id int64) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE attempts SET boundary_observed=1 WHERE chat=(SELECT chat FROM attempts WHERE id=?) AND anchor=(SELECT anchor FROM attempts WHERE id=?) AND anchor_key=(SELECT anchor_key FROM attempts WHERE id=?)", id, id, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=(SELECT chat FROM attempts WHERE id=?)", id)
		return err
	})
}
func (s *Store) InterruptAttempts() error {
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE chats SET generation=generation+1 WHERE id IN(SELECT chat FROM attempts WHERE state='running')"); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE attempts SET finished=?,state='unconfirmed',detail='Execução interrompida; recuperação não confirmada.' WHERE state='running'", time.Now().Unix())
		return err
	})
}
func readAttempts(tx *sql.Tx, id string) ([]Attempt, error) {
	rows, err := tx.Query("SELECT id,chat,started,finished,anchor,anchor_key,state,detail,boundary_observed FROM attempts WHERE chat=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	var out []Attempt
	for rows.Next() {
		var a Attempt
		var start, end int64
		if err = rows.Scan(&a.ID, &a.Chat, &start, &end, &a.Anchor, &a.AnchorKey, &a.State, &a.Detail, &a.BoundaryObserved); err != nil {
			return nil, err
		}
		a.Started = time.Unix(start, 0).UTC()
		if end != 0 {
			a.Finished = time.Unix(end, 0).UTC()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) RetiredFiles(chat string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT file FROM retired_exports WHERE chat=? AND file NOT IN(SELECT file FROM chats)", chat)
	if err != nil {
		return nil, err
	}
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var file string
		if err = rows.Scan(&file); err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}
func (s *Store) ForgetRetired(file string) error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM retired_exports WHERE file=?", file)
		return err
	})
}
