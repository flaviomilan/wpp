package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { must(t, s.Close()) })
	return s
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func message(id string, second int64) Message {
	return Message{Chat: "group@g.us", ID: id, Sender: "person@s.whatsapp.net", Group: true, Kind: "texto", Text: "texto " + id, Time: time.Unix(second, 0).UTC(), Wire: []byte("{}")}
}
func ingest(t *testing.T, s *Store, m ...Message) BatchResult {
	t.Helper()
	r, err := s.Ingest(m)
	must(t, err)
	return r
}

func TestReplayOrderingAndDurableRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("new", 30), message("old", 10))
	if r := ingest(t, s, message("new", 30), message("late", 20)); r.Inserted != 1 || r.Overlap != 1 {
		t.Fatalf("result: %+v", r)
	}
	must(t, s.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	c, m, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if !c.Tracked || len(m) != 3 || m[0].ID != "old" || m[1].ID != "late" || m[2].ID != "new" {
		t.Fatalf("snapshot: %+v %+v", c, m)
	}
	chats, err := s.Chats()
	must(t, err)
	if chats[0].First.Unix() != 10 || chats[0].Last.Unix() != 30 {
		t.Fatalf("coverage: %+v", chats[0])
	}
}
func TestBatchRollbackIncludesSeed(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	_, err := s.Ingest([]Message{message("valid", 30), message("", 40)})
	if err == nil {
		t.Fatal("expected invalid key to abort transaction")
	}
	c, m, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(m) != 0 || c.Generation != 1 {
		t.Fatalf("partial transaction: %+v %+v", c, m)
	}
	chats, err := s.Chats()
	must(t, err)
	if chats[0].Seed != nil {
		t.Fatal("seed committed from rolled back batch")
	}
}
func TestDiscoveryAndUntracking(t *testing.T) {
	s := testStore(t)
	ingest(t, s, message("seed", 30), message("older", 20))
	chats, err := s.Chats()
	must(t, err)
	if chats[0].Count != 0 || chats[0].Seed.ID != "seed" {
		t.Fatalf("untracked chat archived: %+v", chats[0])
	}
	must(t, s.Track("group@g.us", true))
	must(t, s.Track("group@g.us", false))
	ingest(t, s, message("ignored", 40))
	_, m, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(m) != 1 {
		t.Fatal("untracked ingestion")
	}
}
func TestFullKeyAndAliasRekey(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	a := message("same", 10)
	a.Sender = "123@lid"
	b := a
	b.Sender = "456@s.whatsapp.net"
	own := b
	own.FromMe = true
	ingest(t, s, a, b, own)
	must(t, s.Alias(a.Sender, b.Sender))
	_, m, _, err := s.Snapshot(a.Chat)
	must(t, err)
	if len(m) != 2 {
		t.Fatalf("aliases not deduplicated: %+v", m)
	}
	for _, v := range m {
		if v.Sender != b.Sender {
			t.Fatal("noncanonical sender")
		}
	}
}
func TestAliasMergesTrackedConversationsAndAttempts(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("123@lid", true))
	a := message("same", 10)
	a.Chat = "123@lid"
	a.Group = false
	a.Sender = a.Chat
	ingest(t, s, a)
	before, _, _, err := s.Snapshot(a.Chat)
	must(t, err)
	id, err := s.BeginAttempt(a.Chat, a.ID, "pending")
	must(t, err)
	b := a
	b.Chat = "456@s.whatsapp.net"
	b.Sender = b.Chat
	must(t, s.Track(b.Chat, true))
	ingest(t, s, b)
	must(t, s.Alias(a.Chat, b.Chat))
	must(t, s.FinishAttempt(id, "not confirmed"))
	c, m, attempts, err := s.Snapshot(a.Chat)
	must(t, err)
	if c.ID != b.Chat || !c.Tracked || len(m) != 1 || len(attempts) != 1 {
		t.Fatalf("merge: %+v %+v %+v", c, m, attempts)
	}
	// Existing canonical archive keeps its own stable output path.
	if c.File == "" || before.File == "" {
		t.Fatal("missing file")
	}
}
func TestEditDeleteBeforeOriginalAndReplay(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	original := message("edit", 10)
	edit := original
	edit.Mutation = true
	edit.Time = time.Unix(20, 0)
	edit.Version = 20
	edit.Edited = true
	edit.Text = "novo"
	ingest(t, s, edit, original, original)
	_, m, _, err := s.Snapshot(original.Chat)
	must(t, err)
	if len(m) != 1 || m[0].Text != "novo" || m[0].Time.Unix() != 10 || m[0].Mutation {
		t.Fatalf("edit: %+v", m)
	}
	stale := edit
	stale.Text = "velho"
	stale.Version = 15
	ingest(t, s, stale)
	revoked := edit
	revoked.Deleted = true
	revoked.Edited = false
	revoked.Version = 30
	ingest(t, s, revoked, original, edit)
	_, m, _, err = s.Snapshot(original.Chat)
	must(t, err)
	if !m[0].Deleted || m[0].Text != "" {
		t.Fatalf("resurrection: %+v", m)
	}
	before := message("deleted-first", 10)
	revoked = before
	revoked.Mutation = true
	revoked.Deleted = true
	revoked.Version = 40
	ingest(t, s, revoked, before)
	_, m, _, err = s.Snapshot(before.Chat)
	must(t, err)
	for _, v := range m {
		if !v.Deleted || v.Mutation {
			t.Fatalf("deletion placeholder: %+v", v)
		}
	}
}
func TestUnavailableCanBeReplaced(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	unavailable := message("x", 10)
	unavailable.Kind = "indisponível"
	unavailable.Text = ""
	ingest(t, s, unavailable, message("x", 10))
	_, m, _, err := s.Snapshot(unavailable.Chat)
	must(t, err)
	if m[0].Kind != "texto" {
		t.Fatal("decryption retry not applied")
	}
}
func TestAccountAndLock(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("a"))
	must(t, s.BindAccount("a"))
	if s.BindAccount("b") == nil {
		t.Fatal("mixed accounts")
	}
	dir := t.TempDir()
	unlock, err := Lock(dir)
	must(t, err)
	if second, err := Lock(dir); err == nil {
		second()
		t.Fatal("second writer acquired lock")
	}
	unlock()
	unlock, err = Lock(dir)
	must(t, err)
	unlock()
}
func TestCrashLeavesAttemptPending(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	_, err := s.BeginAttempt("group@g.us", "previous", "scanning")
	must(t, err)
	must(t, s.InterruptAttempts())
	_, _, a, err := s.Snapshot("group@g.us")
	must(t, err)
	if a[0].State != "unconfirmed" || a[0].Finished.IsZero() {
		t.Fatalf("attempt: %+v", a)
	}
}

func TestExportFailurePreservesArchive(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("x", 10))
	file := filepath.Join(t.TempDir(), "not-directory")
	must(t, os.WriteFile(file, []byte("x"), 0600))
	e := Exporter{Store: s, Dir: file}
	if e.ExportContext(context.Background(), true) == nil {
		t.Fatal("expected export error")
	}
	c, m, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(m) != 1 || c.Generation == c.Exported {
		t.Fatal("lost data on export error")
	}
}

type fakeHistory struct {
	batches [][]Message
	failure error
	calls   int
}

func (f *fakeHistory) History(ctx context.Context, _ Message) ([]Message, error) {
	f.calls++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.failure != nil {
		return nil, f.failure
	}
	if len(f.batches) == 0 {
		return nil, nil
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}
func TestRecoverOfflineGapThenOlderHistory(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	old := message("checkpoint", 20)
	ingest(t, s, old)
	cp, err := Checkpoints(s)
	must(t, err)
	ingest(t, s, message("new", 40))
	source := &fakeHistory{batches: [][]Message{{message("gap", 30), old}, {message("ancient", 10)}, nil}}
	chats, err := s.Chats()
	must(t, err)
	syncer := Synchronizer{Store: s, Source: source}
	must(t, syncer.Recover(context.Background(), chats[0], cp[chats[0].ID]))
	_, m, a, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(m) != 4 || source.calls != 3 || a[0].State != "unconfirmed" || !strings.Contains(a[0].Detail, "Overlap") {
		t.Fatalf("recovery: %+v %+v calls=%d", m, a, source.calls)
	}
}
func TestHistoryTimeoutAndNoProgressRemainUnconfirmed(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, nil} {
		s := testStore(t)
		must(t, s.Track("group@g.us", true))
		m := message("current", 10)
		ingest(t, s, m)
		source := &fakeHistory{failure: failure, batches: [][]Message{{m}}}
		chats, err := s.Chats()
		must(t, err)
		err = (Synchronizer{Store: s, Source: source}).Recover(context.Background(), chats[0], nil)
		if failure != nil && !errors.Is(err, failure) {
			t.Fatal("lost timeout")
		}
		_, _, a, err := s.Snapshot(m.Chat)
		must(t, err)
		if a[0].State != "unconfirmed" || source.calls != 1 {
			t.Fatalf("state: %+v calls=%d", a, source.calls)
		}
	}
}

func TestPendingBoundarySurvivesNewerMessages(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	old := message("old", 10)
	ingest(t, s, old)
	chats, err := s.Chats()
	must(t, err)
	ingest(t, s, message("recent", 30))
	source := &fakeHistory{failure: context.DeadlineExceeded}
	err = (Synchronizer{Store: s, Source: source}).Recover(context.Background(), chats[0], &old)
	if err == nil {
		t.Fatal("missing history error")
	}
	ingest(t, s, message("newer", 40))
	cp, err := Checkpoints(s)
	must(t, err)
	if cp[old.Chat].ID != old.ID {
		t.Fatal("lost unresolved checkpoint")
	}
	_, _, attempts, err := s.Snapshot(old.Chat)
	must(t, err)
	must(t, s.ObserveBoundary(attempts[0].ID))
	cp, err = Checkpoints(s)
	must(t, err)
	if cp[old.Chat].ID != "newer" {
		t.Fatal("resolved boundary retained")
	}
}

func TestEqualTimestampHistoryCanAdvance(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	start := message("a", 10)
	ingest(t, s, start)
	// Message IDs are opaque, not a chronological sequence.
	previous := message("z", 10)
	source := &fakeHistory{batches: [][]Message{{previous}, nil}}
	chats, err := s.Chats()
	must(t, err)
	must(t, (Synchronizer{Store: s, Source: source}).Recover(context.Background(), chats[0], nil))
	_, m, _, err := s.Snapshot(start.Chat)
	must(t, err)
	if len(m) != 2 || source.calls != 2 {
		t.Fatal("same-second page skipped")
	}
}

func TestDebounceAndRename(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("a", 10))
	dir := t.TempDir()
	e := Exporter{Store: s, Dir: dir}
	d := Debouncer{Exporter: e, Delay: 2 * time.Second}
	now := time.Unix(100, 0)
	must(t, d.TickContext(context.Background(), now))
	c, _, _, err := s.Snapshot("group@g.us")
	must(t, err)
	path := filepath.Join(dir, c.File)
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("export before debounce")
	}
	ingest(t, s, message("b", 20))
	must(t, d.TickContext(context.Background(), now.Add(time.Second)))
	must(t, d.TickContext(context.Background(), now.Add(2*time.Second)))
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("debounce not reset")
	}
	must(t, d.TickContext(context.Background(), now.Add(3*time.Second)))
	must(t, s.Discover(c.ID, "New name"))
	must(t, e.ExportContext(context.Background(), false))
	content, err := os.ReadFile(path)
	must(t, err)
	if len(content) == 0 {
		t.Fatal("JSONL export missing")
	}
	current, _, _, err := s.Snapshot(c.ID)
	must(t, err)
	if current.Generation != current.Exported {
		t.Fatal("renamed chat generation was not exported")
	}
}

func TestAliasRetiresDuplicateExport(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	dir := t.TempDir()
	e := Exporter{Store: s, Dir: dir}
	must(t, s.Track("123@lid", true))
	must(t, s.Track("456@s.whatsapp.net", true))
	must(t, e.ExportContext(context.Background(), true))
	must(t, s.Alias("123@lid", "456@s.whatsapp.net"))
	must(t, e.ExportContext(context.Background(), false))
	files, err := os.ReadDir(dir)
	must(t, err)
	if len(files) != 1 {
		t.Fatalf("duplicate exports: %v", files)
	}
}

func TestConcurrentIngestionAndExport(t *testing.T) {
	s := testStore(t)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	e := Exporter{Store: s, Dir: t.TempDir()}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			_, err := s.Ingest([]Message{message(fmt.Sprintf("m-%03d", i), int64(i+1))})
			if err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if err := e.ExportContext(context.Background(), false); err != nil {
				failures <- err
				return
			}
		}
	}()
	wg.Wait()
	close(failures)
	for err := range failures {
		must(t, err)
	}
	must(t, e.ExportContext(context.Background(), false))
	c, m, _, err := s.Snapshot("group@g.us")
	must(t, err)
	if len(m) != 30 || c.Generation != c.Exported {
		t.Fatal("lost concurrent messages or export changes")
	}
}
