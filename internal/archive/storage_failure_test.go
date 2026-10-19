package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

// Deny one SQLite authorization step to simulate a storage failure inside the
// operation, rather than replacing the transaction with a mock that cannot
// exercise SQLite rollback. The authorizer is disabled before checking state.
func sqliteAuthorization(t *testing.T, s *Store, authorize func(int, string, string, string) int) {
	t.Helper()
	conn, err := s.db.Conn(context.Background())
	must(t, err)
	defer func() { must(t, conn.Close()) }()
	must(t, conn.Raw(func(driver any) error {
		sqlite, ok := driver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("unexpected driver %T", driver)
		}
		sqlite.RegisterAuthorizer(authorize)
		return nil
	}))
}

func archiveRows(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	result := map[string][][]any{}
	for _, table := range []string{"meta", "aliases", "chats", "messages", "attempts", "changes", "source_redirects", "audio_jobs", "people_names", "group_people", "service_settings", "service_operations", "retired_exports"} {
		rows, err := s.db.Query("SELECT * FROM " + table + " ORDER BY rowid")
		must(t, err)
		defer func() { must(t, rows.Close()) }()
		columns, err := rows.Columns()
		must(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			must(t, rows.Scan(dest...))
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			result[table] = append(result[table], values)
		}
		must(t, rows.Err())
		must(t, rows.Close())
	}
	return result
}

func storageFixture(t *testing.T) (*Store, *AudioJob, int64) {
	t.Helper()
	s := testStore(t)
	must(t, s.BindAccount("5511@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	must(t, s.Track("other@g.us", false))
	a := audioMessage("audio")
	a.Sender = "123@lid"
	ingest(t, s, a, message("original", 10))
	duplicate := a
	duplicate.Sender = "5511@s.whatsapp.net"
	ingest(t, s, duplicate)
	must(t, s.ObservePerson("group@g.us", "123@lid", "Ana"))
	must(t, s.SetPersonName("group@g.us", "123@lid", "Ana cadastrada"))
	_, err := s.ControlSettings([]byte(`{"capture":false}`))
	must(t, err)
	must(t, s.SaveOperation(Operation{ID: "operation", State: "running", Updated: time.Unix(10, 0)}))
	id, err := s.BeginRecovery("group@g.us", nil, "pendente")
	must(t, err)
	j, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	if j == nil {
		t.Fatal("audio fixture missing")
	}
	return s, j, id
}

func TestStorageFailuresPropagateAndRollBackAtomicOperations(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*Store, *AudioJob, int64) error
	}{
		{"ingest", func(s *Store, j *AudioJob, id int64) error { _, e := s.Ingest([]Message{message("new", 20)}); return e }},
		{"audio ingest", func(s *Store, j *AudioJob, id int64) error {
			_, e := s.Ingest([]Message{audioMessage("newaudio")})
			return e
		}},
		{"alias reconciliation", func(s *Store, j *AudioJob, id int64) error { return s.Alias("123@lid", "5511@s.whatsapp.net") }},
		{"claim audio", func(s *Store, j *AudioJob, id int64) error {
			_, e := s.ClaimAudioSource(time.Now(), time.Minute, "")
			return e
		}},

		{"interrupted retry", func(s *Store, j *AudioJob, id int64) error { return s.NoteAudioRetryInterrupted(j.Source) }},
		{"batch status", func(s *Store, j *AudioJob, id int64) error {
			_, e := s.EventStatus("group@g.us")
			return e
		}},
		{"evidence", func(s *Store, j *AudioJob, id int64) error {
			_, e := (EventBatchService{Store: s, Dir: t.TempDir()}).Evidence("group@g.us", j.Source)
			return e
		}},

		{"people migration", func(s *Store, j *AudioJob, id int64) error { return s.transaction(migratePeople) }},
		{"audio migration", func(s *Store, j *AudioJob, id int64) error { return s.transaction(migrateAudio) }},
		{"control migration", func(s *Store, j *AudioJob, id int64) error { return s.transaction(migrateControl) }},

		{"track", func(s *Store, j *AudioJob, id int64) error { return s.Track("group@g.us", true) }},
		{"discover", func(s *Store, j *AudioJob, id int64) error { return s.Discover("new@g.us", "Nome") }},
		{"account", func(s *Store, j *AudioJob, id int64) error { return s.BindAccount("5511@s.whatsapp.net") }},
		{"name", func(s *Store, j *AudioJob, id int64) error {
			return s.SetPersonName("group@g.us", "123@lid", "Novo nome")
		}},
		{"clear name", func(s *Store, j *AudioJob, id int64) error { return s.SetPersonName("group@g.us", "123@lid", "") }},
		{"observe", func(s *Store, j *AudioJob, id int64) error { return s.ObservePerson("group@g.us", "456@lid", "Bruno") }},
		{"complete audio", func(s *Store, j *AudioJob, id int64) error {
			return s.CompleteAudio(*j, Transcript{Text: "Entrega confirmada", Language: "pt"})
		}},
		{"release audio", func(s *Store, j *AudioJob, id int64) error { return s.ReleaseAudio(*j, "falhou", false) }},
		{"recover worker", func(s *Store, j *AudioJob, id int64) error { return s.RecoverAudioWorker() }},
		{"phase", func(s *Store, j *AudioJob, id int64) error { return s.SetAudioPhase(*j, "transcribing") }},
		{"interrupt operations", func(s *Store, j *AudioJob, id int64) error { return s.InterruptOperations() }},
		{"save operation", func(s *Store, j *AudioJob, id int64) error {
			return s.SaveOperation(Operation{ID: "new", State: "queued"})
		}},
		{"settings", func(s *Store, j *AudioJob, id int64) error { return s.SaveControlSettings([]byte(`{"capture":true}`)) }},
		{"initial settings", func(s *Store, j *AudioJob, id int64) error { _, e := s.ControlSettings([]byte(`{}`)); return e }},
		{"begin recovery", func(s *Store, j *AudioJob, id int64) error {
			_, e := s.BeginRecovery("group@g.us", &Message{ID: "original"}, "new")
			return e
		}},
		{"finish recovery", func(s *Store, j *AudioJob, id int64) error { return s.FinishAttempt(id, "done") }},
		{"observe boundary", func(s *Store, j *AudioJob, id int64) error { return s.ObserveBoundary(id) }},
		{"interrupt attempts", func(s *Store, j *AudioJob, id int64) error { return s.InterruptAttempts() }},
		{"chats", func(s *Store, j *AudioJob, id int64) error { _, e := s.Chats(); return e }},
		{"snapshot", func(s *Store, j *AudioJob, id int64) error { _, _, _, e := s.Snapshot("group@g.us"); return e }},
		{"people", func(s *Store, j *AudioJob, id int64) error { _, e := s.People("group@g.us"); return e }},
		{"names", func(s *Store, j *AudioJob, id int64) error { _, e := s.Names(); return e }},
		{"messages", func(s *Store, j *AudioJob, id int64) error {
			_, e := s.BrowseMessages("group@g.us", "", "123@lid", 0, 50)
			return e
		}},
		{"source", func(s *Store, j *AudioJob, id int64) error { _, e := s.BrowserSource(j.Source); return e }},
		{"audio detail", func(s *Store, j *AudioJob, id int64) error { _, e := s.BrowserAudioSource(j.Source); return e }},
		{"audio page", func(s *Store, j *AudioJob, id int64) error { _, _, e := s.BrowseAudioPage(0, 50, "", ""); return e }},
		{"audio jobs", func(s *Store, j *AudioJob, id int64) error { _, e := s.AudioJobs(0, 50); return e }},
		{"audio summary", func(s *Store, j *AudioJob, id int64) error { _, e := s.AudioSummary(); return e }},
		{"audio retry state", func(s *Store, j *AudioJob, id int64) error { _, e := s.AudioRetryState(j.Source); return e }},
		{"audio snapshot", func(s *Store, j *AudioJob, id int64) error { _, e := s.AudioSnapshot("group@g.us"); return e }},
		{"audio state", func(s *Store, j *AudioJob, id int64) error { _, e := s.AudioState(j.Source); return e }},
		{"operations", func(s *Store, j *AudioJob, id int64) error { _, e := s.Operations(); return e }},
		{"tables", func(s *Store, j *AudioJob, id int64) error { _, e := s.Tables(); return e }},
		{"inspect", func(s *Store, j *AudioJob, id int64) error { _, e := s.InspectTable("messages", 0, 1); return e }},
		{"checkpoints", func(s *Store, j *AudioJob, id int64) error { _, e := Checkpoints(s); return e }},
		{"retired files", func(s *Store, j *AudioJob, id int64) error { _, e := s.RetiredFiles("group@g.us"); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Authorization calls within a SQL statement can vary; enumerate the
			// fixture's real successful path, then inject one failure at every point.
			s, j, id := storageFixture(t)
			steps := 0
			sqliteAuthorization(t, s, func(action int, a, b, c string) int {
				if action == sqlite3.SQLITE_READ || action == sqlite3.SQLITE_INSERT || action == sqlite3.SQLITE_UPDATE || action == sqlite3.SQLITE_DELETE {
					steps++
				}
				return sqlite3.SQLITE_OK
			})
			err := tc.run(s, j, id)
			sqliteAuthorization(t, s, nil)
			must(t, err)
			for failAt := 1; failAt <= steps; failAt++ {
				t.Run(fmt.Sprintf("failure-%03d", failAt), func(t *testing.T) {
					s, j, id := storageFixture(t)
					before := archiveRows(t, s)
					calls := 0
					denied := false
					sqliteAuthorization(t, s, func(action int, a, b, c string) int {
						if action == sqlite3.SQLITE_READ || action == sqlite3.SQLITE_INSERT || action == sqlite3.SQLITE_UPDATE || action == sqlite3.SQLITE_DELETE {
							calls++
							if calls == failAt {
								denied = true
								return sqlite3.SQLITE_DENY
							}
						}
						return sqlite3.SQLITE_OK
					})
					err := tc.run(s, j, id)
					sqliteAuthorization(t, s, nil)
					if denied && err == nil {
						t.Fatal("storage failure swallowed")
					}
					if denied && !reflect.DeepEqual(before, archiveRows(t, s)) {
						a, _ := json.Marshal(before)
						b, _ := json.Marshal(archiveRows(t, s))
						t.Fatalf("partial changes survived failed operation\nbefore %s\nafter %s", a, b)
					}
				})
			}
		})
	}
}

func TestBatchPreparationStorageFailuresRemainRecoverable(t *testing.T) {
	prepare := func(t *testing.T) (*Store, EventBatchService) {
		s, b := testBatches(t)
		ingest(t, s, message("evidence", 1))
		return s, b
	}
	s, b := prepare(t)
	steps := 0
	sqliteAuthorization(t, s, func(action int, a, b, c string) int {
		if action == sqlite3.SQLITE_READ || action == sqlite3.SQLITE_INSERT || action == sqlite3.SQLITE_UPDATE || action == sqlite3.SQLITE_DELETE {
			steps++
		}
		return sqlite3.SQLITE_OK
	})
	_, err := b.Next("group@g.us")
	sqliteAuthorization(t, s, nil)
	must(t, err)
	for failAt := 1; failAt <= steps; failAt++ {
		t.Run(fmt.Sprintf("failure-%03d", failAt), func(t *testing.T) {
			s, b := prepare(t)
			_, messages, _, err := s.Snapshot("group@g.us")
			must(t, err)
			before, err := json.Marshal(messages)
			must(t, err)
			calls := 0
			denied := false
			sqliteAuthorization(t, s, func(action int, a, b, c string) int {
				if action == sqlite3.SQLITE_READ || action == sqlite3.SQLITE_INSERT || action == sqlite3.SQLITE_UPDATE || action == sqlite3.SQLITE_DELETE {
					calls++
					if calls == failAt {
						denied = true
						return sqlite3.SQLITE_DENY
					}
				}
				return sqlite3.SQLITE_OK
			})
			_, err = b.Next("group@g.us")
			sqliteAuthorization(t, s, nil)
			if denied && err == nil {
				t.Fatal("batch storage failure hidden")
			}
			// Failure after the reservation commit may leave an unpublished pending
			// batch. Retrying must materialize it without losing or duplicating data.
			path, err := b.Next("group@g.us")
			must(t, err)
			if path == "" {
				t.Fatal("evidence lost on retry")
			}
			status, err := s.EventStatus("group@g.us")
			must(t, err)
			must(t, b.Ack("group@g.us", *status.Pending))
			status, err = s.EventStatus("group@g.us")
			must(t, err)
			if status.Pending != nil || status.Remaining != 0 {
				t.Fatal(status)
			}
			_, messages, _, err = s.Snapshot("group@g.us")
			must(t, err)
			after, err := json.Marshal(messages)
			must(t, err)
			if string(before) != string(after) {
				t.Fatal("batch failure changed source messages")
			}
		})
	}
}
