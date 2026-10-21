package archive

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"wpp/internal/localization"
)

func TestApplicationLanguage(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	language, err := s.ApplicationLanguage()
	must(t, err)
	if language != "en" {
		t.Fatal(language)
	}
	var count int
	must(t, s.db.QueryRow("SELECT count(*) FROM meta WHERE key='application_language'").Scan(&count))
	if count != 0 {
		t.Fatal("read wrote default")
	}
	must(t, s.SaveApplicationLanguage("pt-BR"))
	if s.SaveApplicationLanguage("pt") == nil {
		t.Fatal("accepted unsupported language")
	}
	must(t, s.Close())
	language, err = ReadApplicationLanguage(dir)
	must(t, err)
	if language != "pt-BR" {
		t.Fatal(language)
	}
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	_, err = s.db.Exec("UPDATE meta SET value='legacy' WHERE key='application_language'")
	must(t, err)
	language, err = s.ApplicationLanguage()
	must(t, err)
	if language != "en" {
		t.Fatal(language)
	}
	_, err = s.ControlSettings([]byte(`{"language":"pt"}`))
	must(t, err)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if e := s.SaveApplicationLanguage("pt-BR"); e != nil {
				t.Error(e)
			}
		})
		wg.Go(func() {
			if e := s.SaveControlSettings([]byte(`{"language":"es"}`)); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	raw, err := s.ControlSettings(nil)
	must(t, err)
	if !bytes.Equal(raw, []byte(`{"language":"es"}`)) {
		t.Fatal(string(raw))
	}
	language, err = s.ApplicationLanguage()
	must(t, err)
	if language != "pt-BR" {
		t.Fatal(language)
	}
	if _, err = ReadApplicationLanguage(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("read missing database succeeded")
	}
}

func TestApplicationLanguageFailures(t *testing.T) {
	s, err := Open(t.TempDir())
	must(t, err)
	must(t, s.Close())
	if _, err = s.ApplicationLanguage(); err == nil {
		t.Fatal("read failure lost")
	}
	if err = s.SaveApplicationLanguage("en"); err == nil {
		t.Fatal("write failure lost")
	}
}

func TestLanguagePreservesArchiveAndPublishedBatches(t *testing.T) {
	s, dir := eventFixture(t)
	ingestEvent(t, s, evidenceMessage("one", "Conteúdo original 👋"))
	audio := evidenceMessage("audio", "caption")
	audio.Kind = "áudio"
	audio.Transcript = &Transcript{Text: "Transcrição original", Language: "pt", Model: "synthetic"}
	ingestEvent(t, s, audio)
	_, err := s.ControlSettings([]byte(`{"language":"pt","capture_enabled":false}`))
	must(t, err)
	batches := EventBatchService{Store: s, Dir: filepath.Join(dir, "exports")}
	path, err := batches.Next("wedding@g.us")
	must(t, err)
	original, err := os.ReadFile(path)
	must(t, err)
	before := archiveRows(t, s)
	cursorBefore, err := s.EventStatus("wedding@g.us")
	must(t, err)
	unlock, err := Lock(dir)
	must(t, err)
	defer unlock()
	for _, language := range []string{"pt-BR", "en"} {
		must(t, s.SaveApplicationLanguage(language))
	}
	if second, err := Lock(dir); err == nil {
		second()
		t.Fatal("language update released capture ownership")
	}
	after := archiveRows(t, s)
	delete(before, "meta")
	delete(after, "meta")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("language selection changed archive or processing tables")
	}
	cursorAfter, err := s.EventStatus("wedding@g.us")
	must(t, err)
	if !reflect.DeepEqual(cursorBefore, cursorAfter) {
		t.Fatal("language changed consumer state")
	}
	replay, err := batches.Next("wedding@g.us")
	must(t, err)
	data, err := os.ReadFile(replay)
	must(t, err)
	if replay != path || !bytes.Equal(original, data) {
		t.Fatal("published batch bytes changed")
	}
	for _, err := range []error{&HistoryError{Err: errors.New("private")}, &PersistenceError{Err: errors.New("private")}, invalidInput("archive.invalid_pagination")} {
		coded, ok := err.(interface{ MessageCode() string })
		if !ok || !localization.Known(coded.MessageCode()) {
			t.Fatal("missing public diagnostic identity")
		}
	}
}

func TestRecoveryMessageIdentityDoesNotRewriteLegacyRecords(t *testing.T) {
	s, dir := eventFixture(t)
	legacy, err := s.BeginAttempt("wedding@g.us", "old", "Historical diagnostic")
	must(t, err)
	must(t, s.FinishAttempt(legacy, "Historical diagnostic"))
	recent, err := s.BeginAttempt("wedding@g.us", "new", "history.receiving")
	must(t, err)
	must(t, s.FinishAttempt(recent, "history.no_earlier_messages"))
	_, _, attempts, err := s.Snapshot("wedding@g.us")
	must(t, err)
	if attempts[0].Detail != "Historical diagnostic" || attempts[0].DetailCode != "" {
		t.Fatal(attempts[0])
	}
	if attempts[1].DetailCode != "history.no_earlier_messages" || attempts[1].Detail == attempts[1].DetailCode {
		t.Fatal(attempts[1])
	}
	// Reading and changing preference leave the stored diagnostic bytes untouched.
	must(t, s.SaveApplicationLanguage("pt-BR"))
	language, err := ReadApplicationLanguage(dir)
	must(t, err)
	if language != "pt-BR" {
		t.Fatal(language)
	}
	var raw string
	must(t, s.db.QueryRow("SELECT detail FROM attempts WHERE id=?", legacy).Scan(&raw))
	if raw != "Historical diagnostic" {
		t.Fatal(raw)
	}
}
