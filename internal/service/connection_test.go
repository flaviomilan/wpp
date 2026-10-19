package service

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"wpp/internal/archive"
)

type failingTransport struct {
	*fakeTransport
	connectError, closeError, discoverError error
}

func (f *failingTransport) Connect(ctx context.Context) error {
	if f.connectError != nil {
		return f.connectError
	}
	return f.fakeTransport.Connect(ctx)
}
func (f *failingTransport) Close() error                   { return errors.Join(f.fakeTransport.Close(), f.closeError) }
func (f *failingTransport) Discover(context.Context) error { return f.discoverError }

func TestConnectionFailureStatesAndBlockedSessionReplacement(t *testing.T) {
	for _, scenario := range []string{"cancel", "unlinked", "error", "factory", "replacement", "close-error"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				b := &broker{}
				b.linked.Store(scenario != "unlinked")
				factory := func(ctx context.Context, dir string, store *archive.Store) (Transport, error) {
					if scenario == "factory" {
						return nil, errors.New("private open failure")
					}
					transport, err := b.factory(ctx, dir, store)
					if err != nil {
						return nil, err
					}
					f := &failingTransport{fakeTransport: transport.(*fakeTransport)}
					if scenario == "error" {
						f.connectError = errors.New("connection refused")
					}
					if scenario == "close-error" {
						f.closeError = errors.New("close failed")
					}
					return f, nil
				}
				c := newConnection(ctx, Options{Factory: factory}, nil, func(Event) {})
				defer func() { cancel(); requireNoError(t, c.close()) }()
				request, stop := context.WithCancel(ctx)
				defer stop()
				if scenario == "cancel" {
					stop()
				}
				transport, release, err := c.acquire(request, true)
				if scenario == "replacement" || scenario == "close-error" {
					if err != nil {
						t.Fatal(err)
					}
					release()
					release()
					synctest.Wait()
					if c.refs != 0 {
						t.Fatal("release not idempotent")
					}
					c.mu.Lock()
					c.blocked = true
					c.mu.Unlock()
					if _, _, err = c.acquire(ctx, true); err == nil {
						t.Fatal("blocked connection accepted")
					}
					transport, release, err = c.acquire(ctx, false)
					if scenario == "close-error" {
						if err == nil {
							t.Fatal("close failure discarded")
						}
						return
					}
					if err != nil || transport == nil || len(b.transports) != 2 {
						t.Fatal("revoked transport not replaced", err)
					}
					release()
					return
				}
				if err == nil || release != nil || transport != nil {
					t.Fatal("invalid acquire succeeded", err)
				}
				status, detail, _ := c.snapshot()
				if scenario == "unlinked" && status != "needs_login" || scenario == "error" && (status != "error" || detail == "") {
					t.Fatal(status, detail)
				}
			})
		})
	}
}

func TestConnectionCopiesQRAndForwardsOfflineError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		b := &broker{}
		var events []Event
		c := newConnection(ctx, Options{Factory: b.factory}, nil, func(e Event) { events = append(events, e) })
		defer func() { cancel(); requireNoError(t, c.close()) }()
		if err := c.open(); err != nil {
			t.Fatal(err)
		}
		raw := []byte("test QR")
		c.setQR(raw)
		raw[0] = 'x'
		copy := c.QR()
		copy[0] = 'y'
		if string(c.QR()) != "test QR" {
			t.Fatal("QR aliases mutable input")
		}
		for _, kind := range []string{"offline", "error", "online"} {
			b.transports[0].events <- Event{Kind: kind}
			synctest.Wait()
			status, _, linked := c.snapshot()
			if kind == "offline" && status != "reconnecting" || kind == "error" && !c.blocked || kind == "online" && (status != "connected" || !linked || c.blocked) {
				t.Fatal(kind, status)
			}
		}
		if len(events) != 3 {
			t.Fatal(events)
		}
	})
}
