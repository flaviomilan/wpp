package archive

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestTypedErrorsRetainCauseAndSafeDescription(t *testing.T) {
	cause := errors.New("private storage detail")
	for _, tc := range []struct {
		name        string
		err         error
		description string
	}{
		{"validation", invalidInput("invalid pagination"), "invalid pagination"},
		{"history", &HistoryError{Err: cause}, "history unconfirmed"},
		{"persistence", &PersistenceError{Err: cause}, "recovery persistence failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.description {
				t.Fatal(tc.err.Error())
			}
			if tc.name != "validation" && !errors.Is(tc.err, cause) {
				t.Fatal("cause lost")
			}
		})
	}
}

func TestQueueSummaryAndInspectorExposeOnlyPublicRecords(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, audioMessage("ready"))
	summary, err := s.AudioSummary()
	must(t, err)
	if summary["pending"] != 1 {
		t.Fatal(summary)
	}
	tables, err := s.Tables()
	must(t, err)
	for _, table := range tables {
		page, err := s.InspectTable(table.Name, 0, 1)
		must(t, err)
		if len(page.Rows) > 1 || len(page.Columns) == 0 {
			t.Fatal(table.Name, page)
		}
		for _, column := range page.Columns {
			if column == "metadata" || column == "token" {
				t.Fatal("private column exposed", column)
			}
		}
	}
	for _, name := range []string{"service_settings", "sqlite_master", "chats;DELETE FROM messages"} {
		if _, err := s.InspectTable(name, 0, 10); err == nil {
			t.Fatal("unsafe inspector", name)
		}
	}
	for _, page := range [][2]int{{-1, 1}, {0, 0}, {0, 101}} {
		if _, err := s.InspectTable("chats", page[0], page[1]); err == nil {
			t.Fatal("pagination accepted", page)
		}
	}
}

func TestPublicInputValidationPreservesRecords(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("kept", 1))
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"missing chat", func() error { _, e := s.BrowseMessages("missing@g.us", "", "", 0, 1); return e }},
		{"negative offset", func() error { _, e := s.BrowseMessages("group@g.us", "", "", -1, 1); return e }},
		{"zero limit", func() error { _, e := s.BrowseMessages("group@g.us", "", "", 0, 0); return e }},
		{"large query", func() error { _, e := s.BrowseMessages("group@g.us", strings.Repeat("a", 513), "", 0, 1); return e }},
		{"invalid group", func() error { _, e := s.People("123@lid"); return e }},
		{"missing group", func() error { _, e := s.People("missing@g.us"); return e }},
		{"missing registry group", func() error { _, e := s.RegisterPeople(PeopleFile{GroupID: "missing@g.us"}); return e }},
		{"invalid registry group", func() error { _, e := s.RegisterPeople(PeopleFile{GroupID: "123@lid"}); return e }},
		{"invalid person", func() error { return s.SetPersonName("group@g.us", "invalid", "") }},
		{"missing person", func() error { return s.SetPersonName("group@g.us", "123@lid", "") }},
		{"invalid name group", func() error { return s.SetPersonName("123@lid", "123@lid", "") }},
		{"queue pagination", func() error { _, e := s.AudioJobs(-1, 1); return e }},
		{"queue view pagination", func() error { _, _, e := s.BrowseAudioPage(0, 102, "", ""); return e }},
		{"missing source", func() error { _, e := s.BrowserSource("absent"); return e }},
		{"missing audio", func() error { _, e := s.BrowserAudioSource("absent"); return e }},
		{"retry absent", func() error { return s.RetryAudio("absent") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	for _, name := range []string{string([]byte{255}), strings.Repeat("á", 129), "Ana\x00Silva", "Ana\tSilva"} {
		if _, err := s.RegisterPeople(PeopleFile{GroupID: "group@g.us", People: []Person{{ID: "123@lid", Name: name}}}); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	_, messages, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(messages) != 1 || messages[0].ID != "kept" {
		t.Fatal("invalid request changed archive", messages)
	}
}

func TestAudioRetryStateAndCompletionValidation(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, audioMessage("failed"))
	j, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	if j == nil {
		t.Fatal("missing job")
	}
	for _, tc := range []struct {
		name       string
		transcript Transcript
	}{
		{"empty", Transcript{Text: "  "}}, {"invalid UTF8", Transcript{Text: string([]byte{255})}},
		{"too large", Transcript{Text: strings.Repeat("x", 4*1024*1024+1)}},
		{"negative start", Transcript{Text: "ok", Segments: []TranscriptSegment{{StartMS: -1, EndMS: 1}}}},
		{"reverse interval", Transcript{Text: "ok", Segments: []TranscriptSegment{{StartMS: 2, EndMS: 1}}}},
		{"invalid segment text", Transcript{Text: "ok", Segments: []TranscriptSegment{{Text: string([]byte{255})}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.CompleteAudio(*j, tc.transcript); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
	if err := s.ReleaseAudio(*j, strings.Repeat("x", 257), false); err == nil {
		t.Fatal("unbounded categorical error")
	}
	must(t, s.ReleaseAudio(*j, "falhou", false))
	state, err := s.AudioRetryState(j.Source)
	must(t, err)
	if !state.RetryEligible || !state.HasMetadata || !state.Tracked || state.State != "pending" {
		t.Fatal(state)
	}
	must(t, s.Track(state.Chat, false))
	state, err = s.AudioRetryState(j.Source)
	must(t, err)
	if state.Tracked || !state.RetryEligible {
		t.Fatal(state)
	}
	must(t, s.RetryAudio(j.Source))
	must(t, s.NoteAudioRetryInterrupted(j.Source))
	view, err := s.BrowserAudioSource(j.Source)
	must(t, err)
	if !view.RetryEligible || view.RetryReason == "" || view.Error == "" {
		t.Fatal(view)
	}
	if _, err = s.AudioRetryState("absent"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

// A writer which violates io.Writer/WriterString's full-write contract.
type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error)       { return len(p) / 2, nil }
func (shortOutput) WriteString(p string) (int, error) { return len(p) / 2, nil }

func TestExportWriterPreservesFirstFailureAndChecksCancellation(t *testing.T) {
	for _, useString := range []bool{false, true} {
		t.Run(map[bool]string{false: "bytes", true: "string"}[useString], func(t *testing.T) {
			w := exportWriter{writer: shortOutput{}, ctx: context.Background()}
			var err error
			if useString {
				_, err = w.WriteString("conteúdo")
			} else {
				_, err = w.Write([]byte("conteúdo"))
			}
			if !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte("next")); !errors.Is(err, io.ErrShortWrite) {
				t.Fatal("first error lost", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := exportWriter{writer: &bytes.Buffer{}, ctx: ctx}
	if _, err := w.WriteString("x"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Hide WriteString so the fallback byte path is exercised.
	var b bytes.Buffer
	w = exportWriter{writer: struct{ io.Writer }{&b}, ctx: context.Background()}
	_, err := w.WriteString("ação ✅")
	must(t, err)
	if b.String() != "ação ✅" {
		t.Fatal(b.String())
	}
}

func TestAtomicPublicationRejectsDirectoryTargetAndCleansTemporary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing")
	must(t, os.Mkdir(target, 0700))
	if err := AtomicWrite(target, []byte("new")); err == nil {
		t.Fatal("directory replaced")
	}
	files, err := os.ReadDir(dir)
	must(t, err)
	if len(files) != 1 || files[0].Name() != "existing" {
		t.Fatal(files)
	}
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0600))
	if _, err := Open(filepath.Join(dir, "file", "child")); err == nil {
		t.Fatal("non-directory accepted")
	}
}

func TestCorruptControlRecordIsReportedWithoutPartialInterruption(t *testing.T) {
	s := testStore(t)
	op := Operation{ID: "good", State: "running", Updated: time.Unix(10, 0)}
	must(t, s.SaveOperation(op))
	_, err := s.db.Exec("INSERT INTO service_operations(id,data,updated) VALUES('broken','{',0)")
	must(t, err)
	if _, err = s.Operations(); err == nil {
		t.Fatal("corrupt operation hidden")
	}
	if err = s.InterruptOperations(); err == nil {
		t.Fatal("corrupt operation hidden")
	}
	var raw []byte
	must(t, s.db.QueryRow("SELECT data FROM service_operations WHERE id='good'").Scan(&raw))
	must(t, json.Unmarshal(raw, &op))
	if op.State != "running" {
		t.Fatal("partial interruption committed", op)
	}
	op.Updated = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = s.SaveOperation(op); err == nil {
		t.Fatal("invalid JSON time accepted")
	}
}

func TestHistoryAllContinuesUnconfirmedAndHonorsCancellation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cancelOnError bool
	}{{"continue", false}, {"cancel", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			must(t, s.Track("group@g.us", true))
			must(t, s.Track("other@g.us", true))
			must(t, s.Track("ignored@g.us", false))
			second := message("second", 2)
			second.Chat = "other@g.us"
			ingest(t, s, message("first", 1), second)
			source := &fakeHistory{failure: context.DeadlineExceeded}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			warnings := 0
			err := (Synchronizer{Store: s, Source: source}).All(ctx, nil, func(err error) {
				warnings++
				if err == nil {
					t.Error("missing warning")
				}
				if tc.cancelOnError {
					cancel()
				}
			})
			if tc.cancelOnError {
				if !errors.Is(err, context.Canceled) || warnings != 1 || source.calls != 1 {
					t.Fatal(err, warnings, source.calls)
				}
			} else {
				must(t, err)
				if warnings != 2 || source.calls != 2 {
					t.Fatal(warnings, source.calls)
				}
			}
			chats, err := s.Chats()
			must(t, err)
			for _, chat := range chats {
				if chat.ID == "ignored@g.us" {
					_, _, attempts, err := s.Snapshot(chat.ID)
					must(t, err)
					if len(attempts) != 0 {
						t.Fatal("untracked group synchronized")
					}
				}
			}
		})
	}
	t.Run("persistence failure stops", func(t *testing.T) {
		s := testStore(t)
		must(t, s.Track("group@g.us", true))
		ingest(t, s, message("seed", 1))
		_, err := s.db.Exec("CREATE TRIGGER prevent_finish BEFORE UPDATE ON attempts BEGIN SELECT RAISE(ABORT,'disk failure'); END")
		must(t, err)
		warnings := 0
		err = (Synchronizer{Store: s, Source: &fakeHistory{}}).All(context.Background(), nil, func(error) { warnings++ })
		var failure *PersistenceError
		if !errors.As(err, &failure) || warnings != 0 {
			t.Fatal(err, warnings)
		}
	})
	t.Run("cancelled before recovery", func(t *testing.T) {
		s := testStore(t)
		must(t, s.Track("group@g.us", true))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := (Synchronizer{Store: s, Source: &fakeHistory{}}).All(ctx, nil, func(error) { t.Fatal("unexpected warning") }); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestHistoryDelayCompletesOrCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		must(t, pause(context.Background(), time.Second))
		if time.Since(start) != time.Second {
			t.Fatal("delay not honored")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := pause(ctx, time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestAudioWorkerLockRejectsConcurrentReservation(t *testing.T) {
	dir := t.TempDir()
	release, err := AudioLock(dir)
	must(t, err)
	if other, err := AudioLock(dir); err == nil {
		other()
		release()
		t.Fatal("two workers allowed")
	}
	release()
	next, err := AudioLock(dir)
	must(t, err)
	next()
	must(t, os.WriteFile(filepath.Join(dir, "not-directory"), []byte("x"), 0600))
	if unlock, err := AudioLock(filepath.Join(dir, "not-directory", "child")); err == nil {
		unlock()
		t.Fatal("invalid directory accepted")
	}
	must(t, os.Mkdir(filepath.Join(dir, "wpp.lock"), 0700))
	if unlock, err := Lock(dir); err == nil {
		unlock()
		t.Fatal("directory lockfile accepted")
	}
}

func TestQuotedMessageContentKindsAndSelfAttribution(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original Message
		preview  string
	}{
		{"deleted", Message{Deleted: true, Kind: "texto"}, "Mensagem excluída"},
		{"audio", Message{Kind: "áudio", Transcript: &Transcript{Text: "Confirmado ✅"}}, "Áudio · Confirmado ✅"},
		{"document", Message{Kind: "documento", Text: "Ata", FileName: "ata.pdf"}, "[documento] Ata · ata.pdf"},
		{"empty", Message{Kind: "texto"}, "[texto]"},
		{"self", Message{Kind: "texto", Text: "Eu confirmei", FromMe: true}, "Eu confirmei"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			must(t, s.Track("group@g.us", true))
			m := tc.original
			m.Chat = "group@g.us"
			m.ID = "original"
			m.Sender = "123@lid"
			m.Group = true
			m.Time = time.Unix(1, 0)
			response := message("response", 2)
			response.ReplyID = m.ID
			ingest(t, s, m, response)
			page, err := s.BrowseMessages(m.Chat, "", "", 0, 1)
			must(t, err)
			if page.Items[0].Reply == nil || page.Items[0].Reply.Preview != tc.preview {
				t.Fatal(page.Items)
			}
			if m.FromMe {
				if page.Items[0].Reply.Author != "Você" {
					t.Fatal(page.Items)
				}
				detail, err := s.BrowserSource(page.Items[0].Reply.Source)
				must(t, err)
				if detail.Author != "Você" {
					t.Fatal(detail)
				}
			}
		})
	}
}

func TestPeopleRegistrationSkipsEmptyAndRejectsConflictingDuplicates(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	count, err := s.RegisterPeople(PeopleFile{GroupID: "group@g.us", People: []Person{{ID: "123@lid", Name: "Ana"}, {ID: "123@lid", Name: "Ana"}, {ID: "456@lid", Name: "  "}}})
	must(t, err)
	if count != 1 {
		t.Fatal(count)
	}
	if _, err = s.RegisterPeople(PeopleFile{GroupID: "group@g.us", People: []Person{{ID: "123@lid", Name: "Ana"}, {ID: "123@lid", Name: "Bruna"}}}); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	if _, err = s.RegisterPeople(PeopleFile{GroupID: "group@g.us", People: []Person{{ID: "123 /@lid", Name: "Ana"}}}); err == nil {
		t.Fatal("invalid ID accepted")
	}
	must(t, s.BindAccount("123@lid"))
	names, err := s.Names()
	must(t, err)
	if names["self_name"] != "Ana (você)" {
		t.Fatal(names)
	}
	if personLabel(nil, "123@lid", "fallback") != "fallback" {
		t.Fatal("fallback dropped")
	}
	if got := route(Message{Chat: "unlisted", Sender: "123@lid", Kind: "texto"}, nil); !strings.Contains(got, "Você") {
		t.Fatal(got)
	}
}

func TestDebouncedExportFailureCanBeRetried(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	must(t, s.Discover("ignored@g.us", "Ignore"))
	ingest(t, s, message("kept", 1))
	path := filepath.Join(t.TempDir(), "notdir")
	must(t, os.WriteFile(path, []byte("x"), 0600))
	d := Debouncer{Exporter: Exporter{Store: s, Dir: path}, Delay: time.Second}
	now := time.Unix(100, 0)
	must(t, d.TickContext(context.Background(), now))
	if err := d.TickContext(context.Background(), now.Add(time.Second)); err == nil {
		t.Fatal("failed publication hidden")
	}
	d.Exporter.Dir = t.TempDir()
	must(t, d.TickContext(context.Background(), now.Add(2*time.Second)))
	must(t, d.TickContext(context.Background(), now.Add(3*time.Second)))
	if len(d.pending) != 0 {
		t.Fatal("published export still pending")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Exporter.ExportContext(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := d.Exporter.ExportChatContext(context.Background(), "missing@g.us"); err == nil {
		t.Fatal("missing export accepted")
	}
	must(t, s.Close())
	if err := d.TickContext(context.Background(), now); err == nil {
		t.Fatal("closed archive hidden")
	}
}

func TestCorruptStoredContentIsReportedRatherThanSilentlySkipped(t *testing.T) {
	for _, tc := range []struct {
		name       string
		corruption string
		run        func(*Store) error
	}{
		{"chat seed", `UPDATE chats SET seed='{' WHERE id='group@g.us'`, func(s *Store) error { _, e := s.Chats(); return e }},
		{"tracking seed", `UPDATE chats SET seed='{' WHERE id='group@g.us'`, func(s *Store) error { return s.Track("group@g.us", true) }},
		{"ingestion seed", `UPDATE chats SET seed='{' WHERE id='group@g.us'`, func(s *Store) error { _, e := s.Ingest([]Message{message("new", 2)}); return e }},
		{"stored message", `UPDATE messages SET data='{'`, func(s *Store) error { _, _, _, e := s.Snapshot("group@g.us"); return e }},
		{"message update", `UPDATE messages SET data='{'`, func(s *Store) error { _, e := s.Ingest([]Message{message("kept", 1)}); return e }},
		{"public source", `UPDATE messages SET data='{"Time":"invalid"}'`, func(s *Store) error {
			var id string
			if e := s.db.QueryRow("SELECT source_id FROM messages LIMIT 1").Scan(&id); e != nil {
				return e
			}
			_, e := s.BrowserSource(id)
			return e
		}},
		{"message list", `UPDATE messages SET data='{"Time":"invalid"}'`, func(s *Store) error { _, e := s.BrowseMessages("group@g.us", "", "", 0, 50); return e }},

		{"redirect cycle", `INSERT INTO source_redirects(old_id,new_id) VALUES('first','second'),('second','first')`, func(s *Store) error { _, e := s.BrowserSource("first"); return e }},
		{"recovery timestamp", `UPDATE attempts SET started='not-a-number'`, func(s *Store) error { _, _, _, e := s.Snapshot("group@g.us"); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			must(t, s.Track("group@g.us", true))
			ingest(t, s, message("kept", 1))
			_, err := s.BeginRecovery("group@g.us", nil, "pendente")
			must(t, err)
			_, err = s.db.Exec(tc.corruption)
			must(t, err)
			if err = tc.run(s); err == nil {
				t.Fatal("corruption silently accepted")
			}
		})
	}
}

func TestClaimSelectedSourceResolvesRedirects(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, audioMessage("kept"))
	jobs, err := s.AudioJobs(0, 10)
	must(t, err)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	_, err = s.db.Exec("INSERT INTO source_redirects(old_id,new_id) VALUES('old',?)", jobs[0].Source)
	must(t, err)
	job, err := s.ClaimAudioSource(time.Now(), time.Minute, "old")
	must(t, err)
	if job == nil || job.Source != jobs[0].Source {
		t.Fatal(job)
	}
	_, err = s.db.Exec("INSERT INTO source_redirects(old_id,new_id) VALUES('loop','loop')")
	must(t, err)
	if _, err = s.ClaimAudioSource(time.Now(), time.Minute, "loop"); err == nil {
		t.Fatal("redirect loop accepted")
	}
	if err = s.RetryAudio("loop"); err == nil {
		t.Fatal("retry redirect loop accepted")
	}

}
