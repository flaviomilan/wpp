package archive

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func changesFor(t *testing.T, s *Store) []Change {
	t.Helper()
	var changes []Change
	must(t, s.transaction(func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT seq,source_id,revision,operation,data FROM changes ORDER BY seq")
		if err != nil {
			return err
		}
		changes, err = scanChanges(rows)
		return err
	}))
	return changes
}

func TestChangesReplayCorrectionsAndRollback(t *testing.T) {
	s, _ := testBatches(t)
	original := message("original", 20)
	ingest(t, s, original, original)
	edit := original
	edit.Mutation = true
	edit.Edited = true
	edit.Text = "corrigido"
	edit.Version = 30
	ingest(t, s, edit, edit, original)
	deleted := edit
	deleted.Edited = false
	deleted.Deleted = true
	deleted.Version = 40
	ingest(t, s, deleted, original)
	unavailable := message("encrypted", 50)
	unavailable.Kind = "indisponível"
	unavailable.Text = ""
	ingest(t, s, unavailable, message("encrypted", 50))
	ingest(t, s, message("late", 10))
	changes := changesFor(t, s)
	if len(changes) != 6 {
		t.Fatalf("replays generated changes: %+v", changes)
	}
	for i, op := range []string{"insert", "edit", "delete", "insert", "recover", "insert"} {
		if changes[i].Operation != op {
			t.Fatalf("operation[%d]=%s", i, changes[i].Operation)
		}
	}
	if changes[0].Source != changes[2].Source || changes[2].Revision != 3 {
		t.Fatal("unstable revisions")
	}
	_, err := s.Ingest([]Message{message("rollback", 60), message("", 70)})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if len(changesFor(t, s)) != 6 {
		t.Fatal("journal escaped transaction")
	}
}

func TestMigrationFailureRollsBackSourceColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "archive.db"))
	must(t, err)
	statements := []string{
		"CREATE TABLE messages(chat TEXT,id TEXT,sender TEXT,from_me INTEGER,ts INTEGER,data BLOB)",
		"INSERT INTO messages VALUES('group@g.us','m','person',0,10,'invalid JSON')",
		"PRAGMA user_version=1",
	}
	for _, stmt := range statements {
		_, err = db.Exec(stmt)
		must(t, err)
	}
	must(t, db.Close())
	if opened, err := Open(dir); err == nil {
		must(t, opened.Close())
		t.Fatal("invalid baseline accepted")
	}
	db, err = sql.Open("sqlite3", filepath.Join(dir, "archive.db"))
	must(t, err)
	defer func() { must(t, db.Close()) }()
	rows, err := db.Query("PRAGMA table_info(messages)")
	must(t, err)
	defer func() { must(t, rows.Close()) }()
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		must(t, rows.Scan(&cid, &name, &kind, &nn, &def, &pk))
		if name == "source_id" {
			t.Fatal("failed migration left partial column")
		}
	}
	must(t, rows.Err())
}

func TestMigrationBaselinesExistingMessagesDeterministically(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("new", 30), message("old", 10))
	must(t, s.Close())
	db, err := sql.Open("sqlite3", filepath.Join(dir, "archive.db"))
	must(t, err)
	for _, stmt := range []string{"DROP INDEX message_source", "DROP TABLE changes", "DROP TABLE source_redirects", "DROP TABLE analysis_state", "DROP TABLE analysis_batches", "ALTER TABLE messages DROP COLUMN source_id", "PRAGMA user_version=1"} {
		_, err = db.Exec(stmt)
		must(t, err)
	}
	must(t, db.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	changes := changesFor(t, s)
	if len(changes) != 2 || changes[0].Message.ID != "old" || changes[1].Message.ID != "new" {
		t.Fatalf("baseline order: %+v", changes)
	}
	var version int
	must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != 7 {
		t.Fatal("migration not completed")
	}
}

func testBatches(t *testing.T) (*Store, EventBatchService) {
	t.Helper()
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	return s, EventBatchService{Store: s, Dir: t.TempDir()}
}
func nextLot(t *testing.T, b EventBatchService) (string, EventManifest, []byte) {
	t.Helper()
	path, err := b.Next("group@g.us")
	must(t, err)
	if path == "" {
		t.Fatal("expected JSONL lot")
	}
	entry, err := os.ReadFile(path)
	must(t, err)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json"))
	must(t, err)
	var manifest EventManifest
	must(t, json.Unmarshal(raw, &manifest))
	return path, manifest, entry
}
