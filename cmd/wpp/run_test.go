package main

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"wpp/internal/archive"
	"wpp/internal/service"
	"wpp/internal/transcription"
)

type commandTransport struct {
	linked    bool
	online    atomic.Bool
	events    chan service.Event
	closeOnce atomic.Bool
	fail      bool
}

func (c *commandTransport) Session() bool   { return c.linked }
func (c *commandTransport) Connected() bool { return c.online.Load() }
func (c *commandTransport) Connect(context.Context) error {
	if c.online.CompareAndSwap(false, true) {
		c.events <- service.Event{Kind: "online"}
		if c.fail {
			c.events <- service.Event{Kind: "error"}
		}
	}
	return nil
}
func (c *commandTransport) Pair(context.Context, func(string)) error { return nil }
func (c *commandTransport) Discover(context.Context) error           { return nil }
func (c *commandTransport) Events() <-chan service.Event             { return c.events }
func (c *commandTransport) Close() error {
	if c.closeOnce.CompareAndSwap(false, true) {
		close(c.events)
	}
	return nil
}
func (c *commandTransport) History(context.Context, archive.Message) ([]archive.Message, error) {
	return nil, nil
}
func (c *commandTransport) DownloadAudio(context.Context, []byte, string) error {
	return errors.New("no audio fixture")
}

func TestRunUsesIntegratedControllerAndPreservesCancellation(t *testing.T) {
	for _, scenario := range []string{"unlinked", "empty", "conflict", "cancel", "connection-error", "serve-invalid"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				store, err := archive.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "cancel" || scenario == "connection-error" {
					if err = store.Track("group@g.us", true); err != nil {
						t.Fatal(err)
					}
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
				if scenario == "conflict" {
					unlock, err := archive.Lock(dir)
					if err != nil {
						t.Fatal(err)
					}
					defer unlock()
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				transport := &commandTransport{linked: scenario != "unlinked", events: make(chan service.Event, 16), fail: scenario == "connection-error"}
				opts := executeOptions{service: service.Options{Factory: func(context.Context, string, *archive.Store) (service.Transport, error) { return transport, nil }, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) {
					return nil, errors.New("test engine unavailable")
				}}}
				args := []string{"--data-dir", dir, "--export-dir", t.TempDir()}
				if scenario == "empty" {
					args = append(args, "--whisper-model", "fixture-model")
				}
				if scenario == "serve-invalid" {
					args = append(args, "--listen", "example.com:8080", "serve")
				} else {
					args = append(args, "run")
				}
				if scenario == "cancel" {
					go func() { time.Sleep(2 * time.Second); cancel() }()
				}
				var out, stderr bytes.Buffer
				err = executeWithOptions(ctx, args, &out, &stderr, opts)
				if err == nil {
					t.Fatal("invalid run accepted", scenario)
				}
				if scenario == "cancel" {
					if !errors.Is(err, context.Canceled) || !bytes.Contains(out.Bytes(), []byte("Capture requested")) {
						t.Fatal(out.String(), err)
					}
				}
				if scenario == "conflict" && !errors.Is(err, service.ErrReadOnly) {
					t.Fatal(err)
				}
				if scenario == "connection-error" && errors.Is(err, context.Canceled) {
					t.Fatal("connection error became cancellation", err)
				}
			})
		})
	}
}
