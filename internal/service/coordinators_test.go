package service

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
	"wpp/internal/archive"
)

func TestHistorySeedAndPause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &historyCoordinator{}
		ctx, cancel, started := h.begin(context.Background())
		if !started || !h.active() || h.ready() {
			t.Fatal("history not started")
		}
		defer cancel()
		if _, _, started = h.begin(context.Background()); started {
			t.Fatal("overlapping history")
		}
		h.pause()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("history not cancelled")
		}
		h.finish()
		if h.ready() {
			t.Fatal("retry delay lost")
		}
		h.awaitSeed("group@g.us")
		h.seedObserved("different@g.us")
		if h.ready() {
			t.Fatal("unrelated seed removed delay")
		}
		h.seedObserved("group@g.us")
		if !h.ready() || len(h.waiting) != 0 {
			t.Fatal("seed did not request immediate history")
		}
		cancelled, stop := context.WithCancel(context.Background())
		stop()
		if _, _, started = h.begin(cancelled); started {
			t.Fatal("cancelled history started")
		}
	})
}

func TestStorageFaultStopsHistoryWithoutLeakingError(t *testing.T) {
	c := &Controller{history: historyCoordinator{}}
	ctx, cancel, _ := c.history.begin(context.Background())
	defer cancel()
	failure := errors.New("private database detail")
	c.storageFailure(nil)
	if c.blocked {
		t.Fatal("nil error blocked service")
	}
	c.storageFailure(failure)
	if !c.blocked || !errors.Is(c.storageError, failure) || c.connection != "error" || ctx.Err() == nil || c.errorText == failure.Error() {
		t.Fatal("storage failure not contained")
	}
	if operationError(&archive.PersistenceError{Err: failure}) == failure.Error() || operationError(failure) == failure.Error() {
		t.Fatal("private error exposed")
	}
}

func TestDiscoveryUsesTemporaryConnection(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, _, _ := setup(t, b, &fakeEngine{})
	op, err := c.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "succeeded" {
		t.Fatal(result)
	}
	wait(t, func() bool { state, err := c.Snapshot(); return err == nil && state.Connection == "disconnected" })
	state, err := c.Snapshot()
	if err != nil || state.Settings.Capture || b.connects.Load() != 1 {
		t.Fatal(state, err)
	}
}

func TestExportErrorsClearAfterSuccessfulRebuild(t *testing.T) {
	b := &broker{}
	c, store, _ := setup(t, b, &fakeEngine{})
	if err := store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.exports.exportContext(ctx, "group@g.us", false); err == nil || c.exports.status() == "" {
		t.Fatal("failure not reported")
	}
	if err := c.exports.export("", true); err != nil || c.exports.status() != "" {
		t.Fatal(err)
	}
	c.exports.tick(context.Background(), time.Now().Add(time.Minute))
	op, err := c.Export("group@g.us")
	if err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "succeeded" {
		t.Fatal(result)
	}
	if _, err = c.Export("invalid"); err == nil {
		t.Fatal("invalid export accepted")
	}
}

func TestUnavailableAudioEngineAndCancelledWait(t *testing.T) {
	a := newAudio()
	a.engineFailed()
	_, _, detail, available := a.snapshot()
	if detail == "" || available || a.engineAvailable() {
		t.Fatal(detail, available)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.awaitEngine(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := a.run(ctx, nil, nil, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(a.workspaceReady)
	if err := a.run(context.Background(), nil, nil, "", nil); err == nil {
		t.Fatal("missing workspace accepted")
	}
}
