package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"wpp/internal/archive"
	"wpp/internal/transcription"
)

func TestControlPersistenceFailureRestoresChoices(t *testing.T) {
	c, store, _ := setup(t, &broker{}, &fakeEngine{})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before := c.settings
	if err := c.SetCapture(!before.Capture); !persistenceFailure(err) || c.settings.Capture != before.Capture {
		t.Fatal("capture choice changed on failed write", err)
	}
	if err := c.SetTranscription(!before.Transcription); !persistenceFailure(err) || c.settings.Transcription != before.Transcription {
		t.Fatal("transcription choice changed on failed write", err)
	}
	if err := c.Track("group@g.us", true); err == nil {
		t.Fatal("closed database accepted tracking")
	}
}

func TestClosedControllerRejectsControlChanges(t *testing.T) {
	c, _, _ := setup(t, &broker{}, &fakeEngine{})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.SetCapture(true); err == nil {
		t.Fatal("closed capture accepted")
	}
	if err := c.SetTranscription(false); err == nil {
		t.Fatal("closed transcription accepted")
	}
	called := false
	c.spawn(func() { called = true })
	if called {
		t.Fatal("closed controller started worker")
	}
}

func TestConstructorRejectsInvalidPersistedSettings(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	if _, err = store.ControlSettings([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveControlSettings([]byte("invalid json")); err != nil {
		t.Fatal(err)
	}
	if controller, err := New(context.Background(), store, Options{DataDir: dir, ExportDir: t.TempDir(), Factory: (&broker{}).factory}); err == nil {
		requireNoError(t, controller.Close())
		t.Fatal("invalid settings accepted")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = New(context.Background(), store, Options{DataDir: dir}); err == nil {
		t.Fatal("closed store accepted")
	}
}

func TestAudioOptionsOverrideSettingsAndEngineFailureIsContained(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	b := &broker{}
	cfg := transcription.Config{Whisper: "test-whisper", FFmpeg: "test-ffmpeg", Model: "model", Language: "en", Prompt: "vocabulary", Threads: 2, Timeout: time.Minute}
	c, err := New(context.Background(), store, Options{DataDir: dir, ExportDir: t.TempDir(), Factory: b.factory, Audio: &cfg, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) {
		return nil, errors.New("private engine failure")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, c.Close()) }()
	<-c.audio.initialReady
	state, err := c.Snapshot()
	if err != nil || state.Settings.Whisper != cfg.Whisper || state.Settings.FFmpeg != cfg.FFmpeg || state.Settings.Model != cfg.Model || state.Settings.Vocabulary != cfg.Prompt || state.Settings.Threads != cfg.Threads || state.Settings.Timeout != "1m0s" || state.AudioAvailable || state.AudioError == "" || state.AudioError == "private engine failure" {
		t.Fatal(state, err)
	}
}

func TestConnectionEventsKeepStorageFaultAndWakeSeed(t *testing.T) {
	c := &Controller{}
	c.history.awaitSeed("group@g.us")
	c.history.next = time.Now().Add(time.Hour)
	c.onConnectionEvent(Event{Kind: "activity", Chat: "group@g.us"})
	if !c.history.ready() {
		t.Fatal("seed ignored")
	}
	c.onConnectionEvent(Event{Kind: "error"})
	if c.connection != "error" || !c.blocked || c.errorText == "" {
		t.Fatal("error event ignored")
	}
	c.onConnectionEvent(Event{Kind: "online"})
	if c.blocked || c.errorText != "" {
		t.Fatal("online did not recover connection")
	}
	c.storageFailure(errors.New("storage"))
	c.onConnectionEvent(Event{Kind: "online"})
	if !c.blocked || c.errorText == "" {
		t.Fatal("connection hid storage fault")
	}
	c.onConnectionEvent(Event{Kind: "offline"})
}

func TestAudioRetryIneligibleReasons(t *testing.T) {
	for _, item := range []archive.AudioRetryState{{State: "done"}, {State: "processing"}, {Deleted: true}, {State: "cancelled"}, {}, {HasMetadata: true, State: "pending"}} {
		if audioRetryReason(item) == "" {
			t.Fatal("missing retry explanation", item)
		}
	}
}

func TestStorageFaultBlocksOperationsUntilCaptureIsExplicitlyResumed(t *testing.T) {
	c, _, _ := setup(t, &broker{}, &fakeEngine{})
	c.storageFailure(errors.New("database failed"))
	if _, err := c.Discover(); !persistenceFailure(err) {
		t.Fatal("operation not blocked", err)
	}
	if _, _, err := c.acquire(context.Background(), true); err == nil {
		t.Fatal("connection not blocked")
	}
	if err := c.processAudio(context.Background(), nil, "source"); !persistenceFailure(err) {
		t.Fatal("audio not blocked", err)
	}
	if err := c.SetCapture(true); err != nil || c.blocked || c.storageError != nil {
		t.Fatal("explicit resume did not clear fault", err)
	}
	if err := c.SetCapture(false); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Discover(); err == nil {
		t.Fatal("closed controller accepted operation")
	}
}

func TestIdleCloseFailureBecomesVisible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &failingTransport{fakeTransport: &fakeTransport{b: &broker{}, events: make(chan Event)}, closeError: errors.New("test close failure")}
	c := &Controller{ctx: ctx}
	c.connections = newConnection(ctx, Options{}, nil, func(Event) {})
	c.connections.client = transport
	c.closeIdle()
	if c.connection != "error" || c.errorText == "" || c.connections.client != nil {
		t.Fatal("close error hidden")
	}
}

func TestSessionOpeningFailureKeepsPanelAvailable(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	c, err := New(context.Background(), store, Options{DataDir: dir, ExportDir: t.TempDir(), Factory: func(context.Context, string, *archive.Store) (Transport, error) {
		return nil, errors.New("private session error")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, c.Close()) }()
	<-c.audio.initialReady
	state, err := c.Snapshot()
	if err != nil || !state.Owner || state.Error == "" || state.Error == "private session error" || state.AudioAvailable || state.AudioError == "" {
		t.Fatal(state, err)
	}
}

func TestSettingsValidateTimeoutAndTrackingIdentifier(t *testing.T) {
	for _, timeout := range []string{"", "invalid", "0s", "-1m"} {
		if _, err := (Settings{Timeout: timeout, Language: "pt"}).audioConfig(); err == nil {
			t.Fatal("invalid timeout accepted", timeout)
		}
	}
	c, _, _ := setup(t, &broker{}, &fakeEngine{})
	if err := c.Track("group name", true); err == nil {
		t.Fatal("nonidentifier tracking accepted")
	}
}
