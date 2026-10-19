package service

import (
	"testing"
	"time"
	"wpp/internal/archive"
)

func failedSource(t *testing.T, s *archive.Store, id string) string {
	t.Helper()
	if err := seed(s, id, time.Now().Unix(), true); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err = s.ReleaseAudio(*job, "transcrição falhou", false); err != nil {
		t.Fatal(err)
	}
	return job.Source
}
func TestRetryOneAudioWhileCapturePausedAndDeduplicates(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	engine := &fakeEngine{entered: make(chan struct{}), release: make(chan struct{})}
	c, s, _ := setup(t, b, engine)
	requireNoError(t, s.Track("group@g.us", true))
	source := failedSource(t, s, "audio")
	op, err := c.RetryAudio(source)
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	duplicate, err := c.RetryAudio(source)
	if err != nil || duplicate.ID != op.ID {
		t.Fatal("duplicate retry", err)
	}
	close(engine.release)
	result := completed(t, c, op.ID)
	if result.State != "succeeded" || result.Source != source || b.downloads.Load() != 1 || b.histories.Load() != 0 {
		t.Fatal(result)
	}
	state, _ := c.Snapshot()
	if state.Settings.Capture {
		t.Fatal("retry resumed capture")
	}
	wait(t, func() bool { state, _ := c.Snapshot(); return state.Connection == "disconnected" })
	if _, err = c.RetryAudio(source); err == nil {
		t.Fatal("completed audio reprocessed")
	}
}

func TestRetryConnectionFailureKeepsSourceRetryable(t *testing.T) {
	b := &broker{}
	c, s, _ := setup(t, b, &fakeEngine{})
	requireNoError(t, s.Track("group@g.us", true))
	source := failedSource(t, s, "audio")
	op, err := c.RetryAudio(source)
	if err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, op.ID); result.State != "failed" {
		t.Fatal(result)
	}
	item, err := s.BrowserAudioSource(source)
	if err != nil || !item.RetryEligible || b.downloads.Load() != 0 {
		t.Fatal(item, err)
	}
}
func TestRetryRespectsTranscriptionPauseAndFailureRemainsRetryable(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	c, s, _ := setup(t, b, &fakeEngine{fail: true})
	requireNoError(t, s.Track("group@g.us", true))
	source := failedSource(t, s, "audio")
	requireNoError(t, c.SetTranscription(false))
	op, err := c.RetryAudio(source)
	if err != nil {
		t.Fatal(err)
	}
	result := completed(t, c, op.ID)
	if result.State != "warning" || b.connects.Load() != 0 || b.downloads.Load() != 0 {
		t.Fatal(result)
	}
	// Restore an actual failed attempt to test the explicit retry path.
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err = s.ReleaseAudio(*job, "transcrição falhou", false); err != nil {
		t.Fatal(err)
	}
	requireNoError(t, c.SetTranscription(true))
	op, err = c.RetryAudio(source)
	if err != nil {
		t.Fatal(err)
	}
	if result = completed(t, c, op.ID); result.State != "warning" {
		t.Fatal(result)
	}
	item, err := s.BrowserAudioSource(source)
	if err != nil || !item.RetryEligible || item.Attempts != 1 {
		t.Fatal(item, err)
	}
	if err = c.Track("group@g.us", false); err != nil {
		t.Fatal(err)
	}
	if _, err = c.RetryAudio(source); err == nil {
		t.Fatal("untracked audio processed")
	}
}
func TestRetryCancellationAndSourcesQueueThroughOneWorker(t *testing.T) {
	b := &broker{}
	b.linked.Store(true)
	engine := &fakeEngine{entered: make(chan struct{}), release: make(chan struct{})}
	c, s, _ := setup(t, b, engine)
	// Startup recovery must finish before the fixture claims and releases jobs.
	<-c.audio.workspaceReady
	requireNoError(t, s.Track("group@g.us", true))
	source1 := failedSource(t, s, "one")
	source2 := failedSource(t, s, "two")
	if source1 == source2 {
		t.Fatal("fixture must create two distinct retry sources")
	}
	first, err := c.RetryAudio(source1)
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	second, err := c.RetryAudio(source2)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		state, _ := c.Snapshot()
		for _, op := range state.Operations {
			if op.ID == second.ID && op.Phase == "waiting_worker" {
				return true
			}
		}
		return false
	})
	if b.downloads.Load() != 1 {
		t.Fatal("concurrent worker")
	}
	if err = c.Cancel(second.ID); err != nil {
		t.Fatal(err)
	}
	if result := completed(t, c, second.ID); result.State != "cancelled" {
		t.Fatal(result)
	}
	close(engine.release)
	if result := completed(t, c, first.ID); result.State != "succeeded" {
		t.Fatal(result)
	}
	if result := completed(t, c, second.ID); result.State != "cancelled" {
		t.Fatal(result)
	}
	if b.downloads.Load() != 1 {
		t.Fatal("cancelled source processed")
	}
}
