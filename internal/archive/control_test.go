package archive

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestSchemaFivePreservesMessagesAndSources(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ingest([]Message{{Chat: "group@g.us", ID: "original", Sender: "123@lid", Group: true, Kind: "texto", Text: "Conteúdo preservado", Time: time.Unix(100, 0)}}); err != nil {
		t.Fatal(err)
	}
	_, before, _, err := s.Snapshot("group@g.us")
	if err != nil {
		t.Fatal(err)
	}
	// Emulate the previous schema after recording a message/source revision.
	if _, err = s.db.Exec("DROP TABLE service_settings; DROP TABLE service_operations; ALTER TABLE audio_jobs DROP COLUMN phase; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { must(t, s.Close()) }()
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal("migration", version, err)
	}
	_, after, _, err := s.Snapshot("group@g.us")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if !bytes.Equal(a, b) {
		t.Fatal("message or source changed during migration")
	}
	raw := []byte(`{"capture_enabled":false}`)
	if settings, err := s.ControlSettings(raw); err != nil || !bytes.Equal(raw, settings) {
		t.Fatal("initial settings", err)
	}
	op := Operation{ID: "interrupted", Kind: "sync", State: "running", Updated: time.Now().UTC()}
	if err = s.SaveOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = s.InterruptOperations(); err != nil {
		t.Fatal(err)
	}
	operations, err := s.Operations()
	if err != nil || len(operations) != 1 || operations[0].State != "interrupted" {
		t.Fatal("interrupted operation", operations, err)
	}
}
