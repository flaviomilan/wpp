package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockedJSONL struct {
	bytes.Buffer
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedJSONL) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.Buffer.Write(p)
}

func TestJSONLSnapshotAllowsCaptureAndPreservesGeneration(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	_, err := s.Ingest([]Message{message("old", 1)})
	must(t, err)
	w := &blockedJSONL{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(w.release) }) }
	defer unblock()
	type result struct {
		chat Chat
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		chat, err := s.writeJSONL(context.Background(), w, "group@g.us", "")
		finished <- result{chat, err}
	}()
	<-w.entered
	captured := make(chan error, 1)
	go func() { _, err := s.Ingest([]Message{message("arrived-during-export", 2)}); captured <- err }()
	select {
	case err := <-captured:
		must(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("export blocked capture")
	}
	unblock()
	done := <-finished
	must(t, done.err)
	if strings.Contains(w.String(), "arrived-during-export") {
		t.Fatal("snapshot included later arrival")
	}
	must(t, s.MarkExported(done.chat.ID, done.chat.Generation))
	chats, err := s.Chats()
	must(t, err)
	if chats[0].Generation == chats[0].Exported {
		t.Fatal("concurrent arrival was marked exported")
	}
}

type rejectedJSONL struct{}

var errRejectedJSONL = errors.New("test writer rejected output")

func (rejectedJSONL) Write([]byte) (int, error) { return 0, errRejectedJSONL }

func TestJSONLWriteFailurePreservesPreviousFile(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	_, err := s.Ingest([]Message{message("old", 1)})
	must(t, err)
	if _, err = s.writeJSONL(context.Background(), rejectedJSONL{}, "group@g.us", ""); !errors.Is(err, errRejectedJSONL) {
		t.Fatal("writer error discarded", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "export.jsonl")
	must(t, os.WriteFile(path, []byte("previous"), 0600))
	for _, scenario := range []struct {
		name  string
		write func(io.Writer) error
	}{
		{"failure", func(w io.Writer) error {
			_, err := w.Write([]byte("partial"))
			return errors.Join(errRejectedJSONL, err)
		}},
		{"cancellation", func(w io.Writer) error {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := s.writeJSONL(ctx, w, "group@g.us", "")
			return err
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if err := atomicWriteStream(path, scenario.write); err == nil {
				t.Fatal("failed export succeeded")
			}
			raw, err := os.ReadFile(path)
			must(t, err)
			if string(raw) != "previous" {
				t.Fatal("previous export replaced", string(raw))
			}
			files, err := os.ReadDir(dir)
			must(t, err)
			if len(files) != 1 {
				t.Fatal("temporary export retained", files)
			}
		})
	}
}

func TestJSONLExportsAreDeterministicAndDoNotConsumeLots(t *testing.T) {
	s, dir := eventFixture(t)
	original := evidenceMessage("original", "Texto *original*\nUnicode 👩🏽‍❤️‍👩🏻")
	ingestEvent(t, s, original)
	edit := original
	edit.Edited = true
	edit.Mutation = true
	edit.Version = original.Time.Add(time.Minute).UnixMilli()
	edit.Text = "Texto editado"
	ingestEvent(t, s, edit)
	out := filepath.Join(dir, "exports")
	e := Exporter{Store: s, Dir: out}
	must(t, e.ExportContext(context.Background(), true))
	chat, _, _, err := s.Snapshot(original.Chat)
	must(t, err)
	if !strings.HasSuffix(chat.File, ".jsonl") {
		t.Fatal(chat.File)
	}
	path := filepath.Join(out, chat.File)
	first, err := os.ReadFile(path)
	must(t, err)
	for _, line := range bytes.Split(bytes.TrimSpace(first), []byte("\n")) {
		validateEventBytes(t, line)
	}
	var created, edited Event
	lines := bytes.Split(bytes.TrimSpace(first), []byte("\n"))
	if len(lines) != 2 {
		t.Fatal(string(first))
	}
	must(t, json.Unmarshal(lines[0], &created))
	must(t, json.Unmarshal(lines[1], &edited))
	if created.Payload["text"] != original.Text || edited.EventType != "message.edited" {
		t.Fatal(created, edited)
	}
	must(t, e.ExportContext(context.Background(), true))
	second, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Equal(first, second) {
		t.Fatal("export not deterministic")
	}
	b := EventBatchService{Store: s, Dir: out}
	lot, err := b.Next(original.Chat)
	must(t, err)
	if !strings.HasSuffix(lot, ".jsonl") {
		t.Fatal(lot)
	}
	source := created.Payload["source_id"].(string)
	evidence, err := b.Evidence(original.Chat, source)
	must(t, err)
	data, err := os.ReadFile(evidence)
	must(t, err)
	if !bytes.Equal(first, data) {
		t.Fatal("source history incomplete")
	}
	state, err := s.EventStatus(original.Chat)
	must(t, err)
	if state.Cursor != 0 || state.Pending == nil {
		t.Fatal("export consumed cursor", state)
	}
	must(t, filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) != ".jsonl" && filepath.Ext(path) != ".json" {
			t.Fatalf("unexpected export: %s", path)
		}
		return nil
	}))
	for _, source := range []string{"missing", "../../arbitrary", ""} {
		if _, err = b.Evidence(original.Chat, source); err == nil {
			t.Fatal("unknown source exported")
		}
	}
	if _, err = b.Evidence("bad", created.Payload["source_id"].(string)); err == nil {
		t.Fatal("invalid group accepted")
	}
	must(t, s.Track("other@g.us", true))
	if _, err = b.Evidence("other@g.us", created.Payload["source_id"].(string)); err == nil {
		t.Fatal("source leaked across groups")
	}
}

func TestJSONLMigrationInvalidatesOldMarkdownGeneration(t *testing.T) {
	s, dir := eventFixture(t)
	ingestEvent(t, s, evidenceMessage("migration", "Evidência"))
	chat, _, _, err := s.Snapshot("wedding@g.us")
	must(t, err)
	legacy := strings.TrimSuffix(chat.File, ".jsonl") + ".md"
	_, err = s.db.Exec("UPDATE chats SET file=?,exported=generation WHERE id=?", legacy, chat.ID)
	must(t, err)
	_, err = s.db.Exec("PRAGMA user_version=6")
	must(t, err)
	out := filepath.Join(dir, "exports")
	must(t, os.MkdirAll(out, 0700))
	must(t, os.WriteFile(filepath.Join(out, legacy), []byte("legacy evidence"), 0600))
	e := Exporter{Store: s, Dir: out}
	must(t, e.ExportContext(context.Background(), false))
	migrated, _, _, err := s.Snapshot(chat.ID)
	must(t, err)
	if !strings.HasSuffix(migrated.File, ".jsonl") || migrated.Generation != migrated.Exported {
		t.Fatal(migrated)
	}
	old, err := os.ReadFile(filepath.Join(out, legacy))
	must(t, err)
	if string(old) != "legacy evidence" {
		t.Fatal("legacy file modified")
	}
	data, err := os.ReadFile(filepath.Join(out, migrated.File))
	must(t, err)
	if !bytes.Contains(data, []byte("message.created")) {
		t.Fatal("JSONL not rebuilt")
	}
	var version int
	must(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	if version != 7 {
		t.Fatal(version)
	}
}

func TestJSONLRejectsCorruptEventsWithoutReplacingPublishedFile(t *testing.T) {
	s, dir := eventFixture(t)
	m := evidenceMessage("safe", "Conteúdo")
	ingestEvent(t, s, m)
	e := Exporter{Store: s, Dir: filepath.Join(dir, "exports")}
	must(t, e.ExportContext(context.Background(), true))
	chat, _, _, err := s.Snapshot(m.Chat)
	must(t, err)
	path := filepath.Join(e.Dir, chat.File)
	before, err := os.ReadFile(path)
	must(t, err)
	for _, bad := range []string{"{", `{}`, `{"schema_version":"99"}`} {
		_, err = s.db.Exec("UPDATE events SET data=?", bad)
		must(t, err)
		if err = e.ExportChatContext(context.Background(), m.Chat); err == nil {
			t.Fatal("corrupt event exported")
		}
		after, err := os.ReadFile(path)
		must(t, err)
		if !bytes.Equal(before, after) {
			t.Fatal("published file replaced by corrupt export")
		}
	}
}

func TestSnapshotOnlyArrivalMakesAutomaticJSONLExportDirty(t *testing.T) {
	s, dir := eventFixture(t)
	m := evidenceMessage("snapshot-only", "Mensagem original")
	ingestEvent(t, s, m)
	e := Exporter{Store: s, Dir: filepath.Join(dir, "exports")}
	must(t, e.ExportContext(context.Background(), true))
	before, _, _, err := s.Snapshot(m.Chat)
	must(t, err)
	m.Evidence = &Observation{ObservedAt: eventTime(time.Now()), Origin: "history", NativeSender: m.Sender, Reactions: []ReactionObservation{{ActorID: "bia@lid", Emoji: nullableText("👍")}}}
	ingestEvent(t, s, m)
	after, _, _, err := s.Snapshot(m.Chat)
	must(t, err)
	if after.Generation == after.Exported || after.Generation <= before.Generation {
		t.Fatal("new snapshot did not invalidate export")
	}
	d := Debouncer{Exporter: e, Delay: time.Second}
	now := time.Now()
	must(t, d.TickContext(context.Background(), now))
	must(t, d.TickContext(context.Background(), now.Add(time.Second)))
	raw, err := os.ReadFile(filepath.Join(e.Dir, after.File))
	must(t, err)
	if !bytes.Contains(raw, []byte("reaction.snapshot")) {
		t.Fatal("automatic export missed snapshot-only arrival")
	}
}
