package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventBatchesReplayAckAndIntegrity(t *testing.T) {
	s, dir := eventFixture(t)
	b := EventBatchService{Store: s, Dir: filepath.Join(dir, "exports")}
	if path, err := b.Next("wedding@g.us"); err != nil || path != "" {
		t.Fatal(path, err)
	}
	ingestEvent(t, s, evidenceMessage("first", "linha 1\nlinha 2"))
	path, err := b.Next("wedding@g.us")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest EventManifest
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Count != 1 || manifest.FileSHA256["events.jsonl"] != digest(original) || bytes.Count(original, []byte("\n")) != 1 {
		t.Fatal(manifest)
	}
	ingestEvent(t, s, evidenceMessage("second", "Mais evidência"))
	if err = os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if b.Ack("wedding@g.us", manifest.BatchID) == nil {
		t.Fatal("tampered lot acknowledged")
	}
	replay, err := b.Next("wedding@g.us")
	if err != nil || replay != path {
		t.Fatal(replay, err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, restored) {
		t.Fatal(string(restored), err)
	}
	for range 2 {
		if err = b.Ack("wedding@g.us", manifest.BatchID); err != nil {
			t.Fatal(err)
		}
	}
	status, err := s.EventStatus("wedding@g.us")
	if err != nil || status.Cursor != manifest.ToCursor || status.Remaining != 1 || status.Pending != nil {
		t.Fatal(status, err)
	}
	next, err := b.Next("wedding@g.us")
	if err != nil || next == path || next == "" {
		t.Fatal(next, err)
	}
	if b.Ack("wedding@g.us", "../../arbitrary") == nil {
		t.Fatal("invalid batch acknowledged")
	}
	if _, err = b.Next("not-a-group"); err == nil {
		t.Fatal("invalid group accepted")
	}
}
func TestEventBatchesWholeLargeRecordsAndPublicationRetry(t *testing.T) {
	s, dir := eventFixture(t)
	b := EventBatchService{Store: s, Dir: filepath.Join(dir, "blocked")}
	huge := strings.Repeat("á", 600000)
	ingestEvent(t, s, evidenceMessage("huge", huge))
	ingestEvent(t, s, evidenceMessage("small", "fim"))
	if err := os.WriteFile(b.Dir, []byte("file instead of directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Next("wedding@g.us"); err == nil {
		t.Fatal("publication should fail")
	}
	status, err := s.EventStatus("wedding@g.us")
	if err != nil || status.Cursor != 0 || status.Pending == nil {
		t.Fatal(status, err)
	}
	b.Dir = filepath.Join(dir, "good")
	path, err := b.Next("wedding@g.us")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Fatal("record fragmented")
	}
	var e Event
	if err = json.Unmarshal(bytes.TrimSpace(data), &e); err != nil {
		t.Fatal(err)
	}
	if e.Payload["text"] != huge {
		t.Fatal("record truncated")
	}
	if err = b.Ack("wedding@g.us", *status.Pending); err != nil {
		t.Fatal(err)
	}
	next, err := b.Next("wedding@g.us")
	if err != nil || next == path {
		t.Fatal(next, err)
	}
}
func TestEventsTransactionRollbackAndAccountScope(t *testing.T) {
	s, _ := eventFixture(t)
	if _, err := s.db.Exec("CREATE TRIGGER fail_event BEFORE INSERT ON events BEGIN SELECT RAISE(FAIL,'injected event failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest([]Message{evidenceMessage("rollback", "não persistir parcialmente")}); err == nil {
		t.Fatal("journal failure ignored")
	}
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_event"); err != nil {
		t.Fatal(err)
	}
	m := evidenceMessage("same", "Original")
	ingestEvent(t, s, m)
	other := m
	other.Chat = "other@g.us"
	if err := s.Track(other.Chat, true); err != nil {
		t.Fatal(err)
	}
	ingestEvent(t, s, other)
	ev := allEvents(t, s)
	if len(ev) != 2 || ev[0].EventID == ev[1].EventID {
		t.Fatal(ev)
	}
	if err := s.BindAccount("other-account"); err == nil {
		t.Fatal("cross-account reuse accepted")
	}
	// Source IDs and account scope are separate from native message identifiers.
	deleted := m
	deleted.Deleted = true
	deleted.Mutation = true
	deleted.Version = m.Time.Add(1000000000).UnixMilli()
	ingestEvent(t, s, deleted)
	ev = allEvents(t, s)
	if ev[2].EventType != "message.deleted" {
		t.Fatal(ev)
	}
	ingestEvent(t, s, m)
	if len(allEvents(t, s)) != 3 {
		t.Fatal("historical original resurrected deleted message")
	}
}
