package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"wpp/internal/archive"
	"wpp/internal/transcription"
)

func TestDiscoveryAndExportFailuresLeaveArchiveUsable(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	b := &broker{}
	b.linked.Store(true)
	factory := func(ctx context.Context, dir string, store *archive.Store) (Transport, error) {
		transport, err := b.factory(ctx, dir, store)
		if err != nil {
			return nil, err
		}
		return &failingTransport{fakeTransport: transport.(*fakeTransport), discoverError: errors.New("private discover failure")}, nil
	}
	blockedExport := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blockedExport, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(context.Background(), store, Options{DataDir: dir, ExportDir: blockedExport, Factory: factory, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) { return &fakeEngine{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// This fixture deliberately uses a regular file as the export directory;
		// shutdown must report that its final export could not be completed.
		if err := c.Close(); err == nil {
			t.Error("shutdown export failure was lost")
		}
	}()
	if err = c.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	for _, start := range []func() (archive.Operation, error){c.Discover, func() (archive.Operation, error) { return c.Export("group@g.us") }} {
		op, err := start()
		if err != nil {
			t.Fatal(err)
		}
		result := completed(t, c, op.ID)
		if result.State != "failed" || result.Detail == "private discover failure" {
			t.Fatal(result)
		}
	}
	if chats, err := store.Chats(); err != nil || len(chats) != 1 || !chats[0].Tracked {
		t.Fatal(chats, err)
	}
}

func TestExportDebouncePreservesFailureUntilDirtyChatIsSaved(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	if err := store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	e := newExports(store, Options{ExportDir: t.TempDir(), Zone: time.UTC})
	now := time.Now()
	e.tick(context.Background(), now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.tick(ctx, now.Add(3*time.Second))
	if e.status() == "" {
		t.Fatal("export cancellation not reported")
	}
	e.tick(context.Background(), now.Add(4*time.Second))
	if e.status() != "" {
		t.Fatal("successful debounce did not clear fault")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	e.tick(context.Background(), now.Add(5*time.Second))
	if e.status() == "" {
		t.Fatal("closed store error lost")
	}
}

func TestRetryWithoutAvailableEngineKeepsAudioRetryable(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, store, _ := setup(t, b, &fakeEngine{})
	<-c.audio.initialReady
	c.audio.mu.Lock()
	c.audio.engine = nil
	c.audio.mu.Unlock()
	if err := store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	source := failedSource(t, store, "audio")
	op, err := c.RetryAudio(source)
	if err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "warning" || result.Phase != "paused" || b.downloads.Load() != 0 {
		t.Fatal(result)
	}
	item, err := store.AudioRetryState(source)
	if err != nil || !item.RetryEligible {
		t.Fatal(item, err)
	}
	if _, err := c.Sync("invalid"); err == nil {
		t.Fatal("invalid sync accepted")
	}
	if _, err := c.Sync("absent@g.us"); err == nil {
		t.Fatal("untracked sync accepted")
	}
	if _, err := c.RetryAudio("absent"); err == nil {
		t.Fatal("absent audio retry accepted")
	}
}
