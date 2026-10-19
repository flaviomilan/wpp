package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"wpp/internal/archive"
	"wpp/internal/transcription"
)

type broker struct {
	linked      atomic.Bool
	downloads   atomic.Int32
	connects    atomic.Int32
	histories   atomic.Int32
	mu          sync.Mutex
	transports  []*fakeTransport
	history     func(context.Context, archive.Message) ([]archive.Message, error)
	pairRelease chan struct{}
	pairError   bool
}
type fakeTransport struct {
	b      *broker
	ready  atomic.Bool
	events chan Event
	closed atomic.Bool
}

func (b *broker) factory(_ context.Context, _ string, _ *archive.Store) (Transport, error) {
	f := &fakeTransport{b: b, events: make(chan Event, 16)}
	b.mu.Lock()
	b.transports = append(b.transports, f)
	b.mu.Unlock()
	return f, nil
}
func (f *fakeTransport) Session() bool   { return f.b.linked.Load() }
func (f *fakeTransport) Connected() bool { return f.ready.Load() }
func (f *fakeTransport) Connect(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !f.Session() {
		return errors.New("QR code necessário")
	}
	if !f.ready.Swap(true) {
		f.b.connects.Add(1)
		f.events <- Event{Kind: "online"}
	}
	return nil
}
func (f *fakeTransport) Pair(ctx context.Context, code func(string)) error {
	code("private-test-qr")
	if f.b.pairError {
		return errors.New("QR code expirado")
	}
	if f.b.pairRelease != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.b.pairRelease:
		}
	}
	f.b.linked.Store(true)
	return nil
}
func (f *fakeTransport) Discover(context.Context) error { return nil }
func (f *fakeTransport) Events() <-chan Event           { return f.events }
func (f *fakeTransport) Close() error {
	if !f.closed.Swap(true) {
		f.ready.Store(false)
		close(f.events)
	}
	return nil
}
func (f *fakeTransport) History(ctx context.Context, m archive.Message) ([]archive.Message, error) {
	f.b.histories.Add(1)
	if f.b.history != nil {
		return f.b.history(ctx, m)
	}
	return nil, nil
}
func (f *fakeTransport) DownloadAudio(_ context.Context, _ []byte, path string) error {
	f.b.downloads.Add(1)
	return os.WriteFile(path, []byte("sample"), 0600)
}

type fakeEngine struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
}

func (e *fakeEngine) Transcribe(ctx context.Context, _ string, _ string) (archive.Transcript, error) {
	if e.entered != nil {
		e.once.Do(func() { close(e.entered) })
	}
	if e.release != nil {
		select {
		case <-ctx.Done():
			return archive.Transcript{}, ctx.Err()
		case <-e.release:
		}
	}
	if e.fail {
		return archive.Transcript{}, errors.New("private engine output")
	}
	return archive.Transcript{Text: "Entrega confirmada", Language: "pt", Model: "test"}, nil
}
func setup(t *testing.T, b *broker, e *fakeEngine) (*Controller, *archive.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	requireNoError(t, store.BindAccount("account@s.whatsapp.net"))
	controller, err := New(context.Background(), store, Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"), Factory: b.factory, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) { return e, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, existingFault := store.Chats()
		shutdownErr := controller.Close()
		if existingFault == nil {
			requireNoError(t, shutdownErr)
		}
		// Persistence-fault tests deliberately close SQLite before shutdown;
		// their shutdown failures are consequences of the already asserted fault.
		requireNoError(t, store.Close())
	})
	return controller, store, dir
}
func wait(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func completed(t *testing.T, c *Controller, id string) archive.Operation {
	t.Helper()
	var result archive.Operation
	wait(t, func() bool {
		state, e := c.Snapshot()
		if e != nil {
			t.Fatal(e)
		}
		for _, op := range state.Operations {
			if op.ID == id && op.State != "queued" && op.State != "running" {
				result = op
				return true
			}
		}
		return false
	})
	return result
}
func seed(store *archive.Store, id string, second int64, audio bool) error {
	m := archive.Message{Chat: "group@g.us", ID: id, Sender: "123@lid", Group: true, Kind: "texto", Text: id, Time: time.Unix(second, 0), Wire: []byte("{}")}
	if audio {
		m.Kind = "áudio"
		m.Text = ""
		m.AudioMetadata = []byte("private metadata")
	}
	_, e := store.Ingest([]archive.Message{m})
	return e
}
func TestManualSyncIsFiniteAndDoesNotEnableCapture(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	engine := &fakeEngine{entered: make(chan struct{}), release: make(chan struct{})}
	c, s, _ := setup(t, b, engine)
	requireNoError(t, s.Track("group@g.us", true))
	requireNoError(t, seed(s, "seed", 100, false))
	b.history = func(_ context.Context, anchor archive.Message) ([]archive.Message, error) {
		if anchor.ID != "seed" {
			return nil, nil
		}
		return []archive.Message{{Chat: "group@g.us", ID: "old-audio", Sender: "123@lid", Group: true, Kind: "áudio", Time: time.Unix(80, 0), Wire: []byte("{}"), AudioMetadata: []byte("keys")}}, nil
	}
	op, err := c.Sync("group@g.us")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := c.Sync("group@g.us")
	if err != nil || duplicate.ID != op.ID {
		t.Fatal("duplicate operation")
	}
	<-engine.entered
	if err = seed(s, "late-audio", 90, true); err != nil {
		t.Fatal(err)
	}
	close(engine.release)
	result := completed(t, c, op.ID)
	if (result.State != "succeeded" && result.State != "warning") || result.Total != 1 || result.Completed != 1 {
		t.Fatal(result)
	}
	state, _ := c.Snapshot()
	if state.Settings.Capture {
		t.Fatal("one shot enabled continuous capture")
	}
	jobs, _ := s.AudioJobs(0, 50)
	done, pending := 0, 0
	for _, j := range jobs {
		if j.State == "done" {
			done++
		}
		if j.State == "pending" {
			pending++
		}
	}
	if done != 1 || pending != 1 || b.downloads.Load() != 1 {
		t.Fatal("snapshot was expanded", jobs)
	}
	wait(t, func() bool { state, _ := c.Snapshot(); return state.Connection == "disconnected" })
}

func TestPairSuccessAndCancelledHistoryKeepArchive(t *testing.T) {
	b := &broker{}
	c, s, _ := setup(t, b, &fakeEngine{})
	op, err := c.Pair()
	if err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "succeeded" {
		t.Fatal(result)
	}
	snapshot, _ := c.Snapshot()
	if !snapshot.Linked || snapshot.Settings.Capture || len(c.QR()) != 0 {
		t.Fatal("pair state", snapshot)
	}
	if err = s.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if err = seed(s, "preserved", 100, false); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	b.history = func(ctx context.Context, _ archive.Message) ([]archive.Message, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	op, err = c.Sync("group@g.us")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err = c.Cancel(op.ID); err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "cancelled" {
		t.Fatal(result)
	}
	_, messages, _, err := s.Snapshot("group@g.us")
	if err != nil || len(messages) != 1 {
		t.Fatal("cancel removed archive", err)
	}
	wait(t, func() bool { state, _ := c.Snapshot(); return state.Connection == "disconnected" })
}

func TestManualSyncDoesNotReserveAfterPauseWhileWorkerBusy(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, s, _ := setup(t, b, &fakeEngine{})
	requireNoError(t, s.Track("group@g.us", true))
	requireNoError(t, seed(s, "audio", 100, true))
	releaseAudio, _ := c.audio.queue.acquire(context.Background(), true)
	op, err := c.Sync("group@g.us")
	if err != nil {
		releaseAudio()
		t.Fatal(err)
	}
	ready := false
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		state, _ := c.Snapshot()
		for _, current := range state.Operations {
			if current.ID == op.ID && current.Phase == "audio" {
				ready = true
			}
		}
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		releaseAudio()
		t.Fatal("manual list did not wait for worker")
	}
	if err = c.SetTranscription(false); err != nil {
		releaseAudio()
		t.Fatal(err)
	}
	releaseAudio()
	result := completed(t, c, op.ID)
	if result.State != "warning" || result.Completed != 0 || b.downloads.Load() != 0 {
		t.Fatal("reserved audio after pause", result)
	}
}
func TestTranscriptionPauseFinishesCurrentJobAndPreservesChoice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &broker{}
		b.linked.Store(true)
		engine := &fakeEngine{entered: make(chan struct{}), release: make(chan struct{})}
		c, s, dir := setup(t, b, engine)
		requireNoError(t, s.Track("group@g.us", true))
		requireNoError(t, seed(s, "one", 10, true))
		requireNoError(t, seed(s, "two", 20, true))
		if err := c.SetCapture(true); err != nil {
			t.Fatal(err)
		}
		<-engine.entered
		if err := c.SetTranscription(false); err != nil {
			t.Fatal(err)
		}
		close(engine.release)
		synctest.Wait()
		wait(t, func() bool {
			jobs, _ := s.AudioJobs(0, 50)
			n := 0
			for _, j := range jobs {
				if j.State == "done" {
					n++
				}
			}
			return n == 1
		})
		// Advance the fake clock past two worker ticks, then await runnable work.
		time.Sleep(time.Second)
		synctest.Wait()
		if b.downloads.Load() != 1 {
			t.Fatal("reserved another audio after pause")
		}
		requireNoError(t, c.SetCapture(false))
		requireNoError(t, c.Close())
		again, err := New(context.Background(), s, Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"), Factory: b.factory, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) { return &fakeEngine{}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { requireNoError(t, again.Close()) }()
		state, _ := again.Snapshot()
		if state.Settings.Capture || state.Settings.Transcription {
			t.Fatal("choices not resumed")
		}
	})
}
func TestPairCancellationExpiryAndNoQRInOperations(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "expiry"}[expiry], func(t *testing.T) {
			b := &broker{pairRelease: make(chan struct{}), pairError: expiry}
			c, _, _ := setup(t, b, &fakeEngine{})
			op, err := c.Pair()
			if err != nil {
				t.Fatal(err)
			}
			if !expiry {
				wait(t, func() bool { return len(c.QR()) > 0 })
				if err = c.Cancel(op.ID); err != nil {
					t.Fatal(err)
				}
			}
			result := completed(t, c, op.ID)
			if expiry && result.State != "failed" {
				t.Fatal(result)
			}
			if !expiry && result.State != "cancelled" {
				t.Fatal(result)
			}
			if len(c.QR()) != 0 {
				t.Fatal("QR retained")
			}
			raw, _ := json.Marshal(result)
			if string(raw) == "" || contains(string(raw), "private-test-qr") {
				t.Fatal("QR exposed")
			}
		})
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func TestLockConflictPreservesControlsAndExternalOperations(t *testing.T) {
	dir := t.TempDir()
	s, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, s.Close()) }()
	unlock, err := archive.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	requireNoError(t, s.SaveOperation(archive.Operation{ID: "external", State: "running", Updated: time.Now()}))
	b := &broker{}
	c, err := New(context.Background(), s, Options{DataDir: dir, Factory: b.factory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, c.Close()) }()
	state, _ := c.Snapshot()
	if state.Owner || len(b.transports) != 0 || state.Operations[0].State != "running" {
		t.Fatal(state)
	}
	if !errors.Is(c.SetCapture(true), ErrReadOnly) {
		t.Fatal("external capture was changed")
	}
	if _, err = c.Sync("group@g.us"); err == nil {
		t.Fatal("external sync allowed")
	}
	if !errors.Is(c.SetTranscription(false), ErrReadOnly) || !errors.Is(c.Track("group@g.us", true), ErrReadOnly) || !errors.Is(c.Cancel("external"), ErrReadOnly) {
		t.Fatal("external controls were changed")
	}
	if _, err = c.RetryAudio("source"); !errors.Is(err, ErrReadOnly) {
		t.Fatal("external retry allowed", err)
	}
}
func TestTrackingWhileActiveAndEngineFailureDoesNotStopCapture(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, s, _ := setup(t, b, &fakeEngine{fail: true})
	if err := c.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	requireNoError(t, seed(s, "one", 1, true))
	requireNoError(t, c.SetCapture(true))
	wait(t, func() bool { return b.downloads.Load() > 0 })
	if err := c.Track("group@g.us", false); err != nil {
		t.Fatal(err)
	}
	state, _ := c.Snapshot()
	if !state.Settings.Capture {
		t.Fatal("engine error stopped capture")
	}
	page, _ := s.BrowseMessages("group@g.us", "", "", 0, 50)
	if len(page.Items) != 1 {
		t.Fatal("untrack removed archive")
	}
}
func TestValidSettingsArePersistentAndInvalidSettingsDoNotApply(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, s, dir := setup(t, b, &fakeEngine{})
	state, _ := c.Snapshot()
	candidate := state.Settings
	candidate.Language = "invalid"
	op, err := c.Configure(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if completed(t, c, op.ID).State != "failed" {
		t.Fatal("invalid settings applied")
	}
	unchanged, err := c.Snapshot()
	if err != nil || unchanged.Settings != state.Settings {
		t.Fatal("failed validation changed persisted settings", unchanged, err)
	}
	candidate = state.Settings
	candidate.Vocabulary = "Aurora, Mariana"
	candidate.Threads = 2
	op, err = c.Configure(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if completed(t, c, op.ID).State != "succeeded" {
		t.Fatal("valid settings failed")
	}
	requireNoError(t, c.SetCapture(true))
	requireNoError(t, c.SetTranscription(false))
	requireNoError(t, c.Close())
	requireNoError(t, s.SaveOperation(archive.Operation{ID: "abandoned", State: "running", Updated: time.Now()}))
	again, err := New(context.Background(), s, Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"), Factory: b.factory, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) { return &fakeEngine{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, again.Close()) }()
	state, _ = again.Snapshot()
	if !state.Settings.Capture || state.Settings.Transcription || state.Settings.Vocabulary != "Aurora, Mariana" || state.Settings.Threads != 2 {
		t.Fatal(state)
	}
	for _, op := range state.Operations {
		if op.ID == "abandoned" && op.State != "interrupted" {
			t.Fatal(op)
		}
	}
}
func TestSessionRevocationStopsConnectionAndPreservesCaptureIntent(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, s, _ := setup(t, b, &fakeEngine{})
	requireNoError(t, c.Track("group@g.us", true))
	requireNoError(t, seed(s, "one", 1, false))
	requireNoError(t, c.SetCapture(true))
	wait(t, func() bool { state, _ := c.Snapshot(); return state.Connection == "connected" })
	b.mu.Lock()
	f := b.transports[len(b.transports)-1]
	b.mu.Unlock()
	b.linked.Store(false)
	f.events <- Event{Kind: "needs_login"}
	wait(t, func() bool { state, _ := c.Snapshot(); return state.Connection == "needs_login" })
	state, _ := c.Snapshot()
	if state.Linked || !state.Settings.Capture {
		t.Fatal(state)
	}
}
