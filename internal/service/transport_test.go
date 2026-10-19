package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"go.mau.fi/whatsmeow"
	"wpp/internal/archive"
	"wpp/internal/whatsapp"
)

type testConnection struct {
	online        atomic.Bool
	fail          error
	qr            <-chan whatsmeow.QRChannelItem
	qrFail        error
	connectOnline bool
}

func (c *testConnection) IsConnected() bool { return c.online.Load() }
func (c *testConnection) ConnectContext(context.Context) error {
	if c.connectOnline {
		c.online.Store(true)
	}
	return c.fail
}
func (c *testConnection) GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return c.qr, c.qrFail
}
func transportFixture(t *testing.T) (*waTransport, *testConnection) {
	t.Helper()
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := whatsapp.Open(context.Background(), dir, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := newWhatsAppTransport(client)
	connection := &testConnection{}
	tr.connection = connection
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Error(err)
		}
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return tr, connection
}
func TestOpenWhatsAppSessionAndForwarding(t *testing.T) {
	dir := t.TempDir()
	s, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	transport, err := openWhatsApp(context.Background(), dir, s)
	if err != nil {
		t.Fatal(err)
	}
	if transport.Session() || transport.Connected() || transport.(*waTransport).AudioReady() {
		t.Fatal("unlinked account reports readiness")
	}
	if err = transport.DownloadAudio(context.Background(), []byte{255}, "unused"); err == nil {
		t.Fatal("bad descriptor accepted")
	}
	if _, err = transport.History(context.Background(), archive.Message{}); err == nil {
		t.Fatal("empty history accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = transport.Discover(ctx); err == nil {
		t.Fatal("offline discovery succeeded")
	}
	if err = transport.Close(); err != nil {
		t.Fatal(err)
	}
	if _, open := <-transport.Events(); open {
		t.Fatal("events not closed")
	}
	if _, err = openWhatsApp(ctx, dir+"/missing", s); err == nil {
		t.Fatal("missing storage accepted")
	}
}
func TestTransportConnectionStates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, conn := transportFixture(t)
		if err := tr.Connect(context.Background()); err == nil {
			t.Fatal("unlinked session connected")
		}
		tr.session.Store(true)
		conn.online.Store(true)
		if err := tr.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		conn.online.Store(false)
		tr.failed.Store(true)
		if err := tr.Connect(context.Background()); err == nil {
			t.Fatal("failed session connected")
		}
		tr.failed.Store(false)
		conn.fail = errors.New("private connection error")
		if err := tr.Connect(context.Background()); err == nil || err.Error() == conn.fail.Error() {
			t.Fatal(err)
		}
		conn.fail = nil
		conn.connectOnline = true
		if err := tr.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		conn.connectOnline = false
		conn.online.Store(false)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := tr.Connect(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := tr.Connect(context.Background()); err == nil {
			t.Fatal("timeout ignored")
		}
		result := make(chan error, 1)
		go func() { result <- tr.Connect(context.Background()) }()
		synctest.Wait()
		tr.failed.Store(true)
		time.Sleep(100 * time.Millisecond)
		if err := <-result; err == nil {
			t.Fatal("failure while connecting lost")
		}
		tr.failed.Store(false)
		go func() { result <- tr.Connect(context.Background()) }()
		synctest.Wait()
		conn.online.Store(true)
		time.Sleep(100 * time.Millisecond)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	})
}
func TestTransportPairing(t *testing.T) {
	tr, conn := transportFixture(t)
	conn.qrFail = errors.New("qr setup")
	if err := tr.Pair(context.Background(), func(string) {}); err == nil {
		t.Fatal("qr failure lost")
	}
	conn.qrFail = nil
	conn.fail = errors.New("connect")
	if err := tr.Pair(context.Background(), func(string) {}); err == nil {
		t.Fatal("connect failure lost")
	}
	conn.fail = nil
	for _, event := range []string{"timeout", "error", "closed", "cancel", "success"} {
		t.Run(event, func(t *testing.T) {
			channel := make(chan whatsmeow.QRChannelItem, 3)
			channel <- whatsmeow.QRChannelItem{Event: "code", Code: "private-test-code"}
			channel <- whatsmeow.QRChannelItem{Event: "unrecognized"}
			if event == "closed" {
				close(channel)
			} else if event != "cancel" {
				channel <- whatsmeow.QRChannelItem{Event: event}
			}
			conn.qr = channel
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var code string
			err := tr.Pair(ctx, func(value string) {
				code = value
				if event == "cancel" {
					cancel()
				}
			})
			if event == "success" {
				if err != nil || !tr.Session() {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("pair failure lost")
			}
			if code != "private-test-code" {
				t.Fatal("qr not delivered")
			}
		})
	}
	if err := tr.Pair(context.Background(), func(string) { t.Fatal("already paired requested QR") }); err != nil {
		t.Fatal(err)
	}
}
func TestTransportPumpAndIdempotentShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, _ := transportFixture(t)
		tr.client.Online <- struct{}{}
		synctest.Wait()
		if e := <-tr.Events(); e.Kind != "online" || !tr.Session() {
			t.Fatal(e)
		}
		tr.client.Offline <- struct{}{}
		synctest.Wait()
		if e := <-tr.Events(); e.Kind != "offline" {
			t.Fatal(e)
		}
		tr.client.Activity <- "group@g.us"
		synctest.Wait()
		if e := <-tr.Events(); e.Kind != "activity" || e.Chat != "group@g.us" {
			t.Fatal(e)
		}
		tr.client.Fatal <- errors.New("network")
		synctest.Wait()
		if e := <-tr.Events(); e.Kind != "error" {
			t.Fatal(e)
		}
		tr.client.Fatal <- whatsapp.ErrSessionRevoked
		synctest.Wait()
		if e := <-tr.Events(); e.Kind != "needs_login" || tr.Session() {
			t.Fatal(e)
		}
		for i := 0; i < cap(tr.events); i++ {
			tr.events <- Event{Kind: "activity"}
		}
		tr.client.Activity <- "drop"
		synctest.Wait()
		tr.client.Online <- struct{}{}
		synctest.Wait()
		result := make(chan error, 2)
		go func() { result <- tr.Close() }()
		go func() { result <- tr.Close() }()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		select {
		case <-tr.done:
		default:
			t.Fatal("close returned before pump finished")
		}
		count := 0
		for range tr.Events() {
			count++
		}
		if count != cap(tr.events) {
			t.Fatal("unexpected queued events", count)
		}
	})
}
