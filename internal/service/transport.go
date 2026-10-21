package service

import (
	"context"
	"errors"
	"go.mau.fi/whatsmeow"
	"sync"
	"sync/atomic"
	"time"
	"wpp/internal/archive"
	"wpp/internal/transcription"
	"wpp/internal/whatsapp"
)

type Event struct{ Kind, Chat string }
type Transport interface {
	archive.HistorySource
	transcription.Downloader
	Session() bool
	Connect(context.Context) error
	Connected() bool
	Pair(context.Context, func(string)) error
	Discover(context.Context) error
	Events() <-chan Event
	Close() error
}
type Factory func(context.Context, string, *archive.Store) (Transport, error)
type connectionClient interface {
	IsConnected() bool
	ConnectContext(context.Context) error
	GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error)
}

type waTransport struct {
	connection connectionClient
	done       chan struct{}
	client     *whatsapp.Client
	events     chan Event
	stop       chan struct{}
	once       sync.Once
	session    atomic.Bool
	failed     atomic.Bool
}

func openWhatsApp(ctx context.Context, dir string, store *archive.Store) (Transport, error) {
	client, err := whatsapp.Open(ctx, dir, store, func(string) {})
	if err != nil {
		return nil, invalid("backend.could_not_open_the_local_session")
	}
	return newWhatsAppTransport(client), nil
}

func newWhatsAppTransport(client *whatsapp.Client) *waTransport {
	t := &waTransport{client: client, connection: client.WA, events: make(chan Event, 64), stop: make(chan struct{}), done: make(chan struct{})}
	t.session.Store(client.WA.Store.ID != nil)
	go func() {
		defer close(t.done)
		defer close(t.events)
		for {
			var event Event
			select {
			case <-t.stop:
				return
			case <-client.Online:
				event.Kind = "online"
				t.session.Store(true)
			case <-client.Offline:
				event.Kind = "offline"
			case err := <-client.Fatal:
				t.failed.Store(true)
				event.Kind = "error"
				if err != nil && errors.Is(err, whatsapp.ErrSessionRevoked) {
					t.session.Store(false)
					event.Kind = "needs_login"
				}
			case id := <-client.Activity:
				event = Event{Kind: "activity", Chat: id}
			}
			if event.Kind == "activity" {
				select {
				case t.events <- event:
				case <-t.stop:
					return
				default:
				}
			} else {
				select {
				case t.events <- event:
				case <-t.stop:
					return
				}
			}
		}
	}()
	return t
}
func (t *waTransport) Session() bool        { return t.session.Load() }
func (t *waTransport) Connected() bool      { return t.connection.IsConnected() }
func (t *waTransport) Events() <-chan Event { return t.events }
func (t *waTransport) Connect(ctx context.Context) error {
	if !t.Session() {
		return invalid("backend.link_your_account_using_the_qr_code_to_continue")
	}
	if t.Connected() {
		return nil
	}
	if t.failed.Load() {
		return invalid("backend.the_connection_was_refused_link_your_account_again")
	}
	if err := t.connection.ConnectContext(ctx); err != nil {
		return invalid("backend.could_not_connect_to_whatsapp")
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(90 * time.Second)
	defer timeout.Stop()
	for {
		if t.Connected() {
			return nil
		}
		if t.failed.Load() {
			return invalid("backend.whatsapp_refused_the_connection")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return invalid("backend.whatsapp_did_not_respond_try_connecting_again")
		case <-tick.C:
		}
	}
}
func (t *waTransport) Pair(ctx context.Context, code func(string)) error {
	if t.Session() {
		return nil
	}
	qr, err := t.connection.GetQRChannel(ctx)
	if err != nil {
		return invalid("backend.could_not_prepare_the_qr_code")
	}
	if err = t.connection.ConnectContext(ctx); err != nil {
		return invalid("backend.could_not_start_pairing")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-qr:
			if !ok {
				return invalid("backend.qr_code_expired_request_another_to_continue")
			}
			switch item.Event {
			case "code":
				code(item.Code)
			case "success":
				t.session.Store(true)
				return nil
			case "timeout", "error":
				return invalid("backend.qr_expired_or_refused")
			}
		}
	}
}
func (t *waTransport) Discover(ctx context.Context) error { return t.client.Discover(ctx) }
func (t *waTransport) History(ctx context.Context, m archive.Message) ([]archive.Message, error) {
	return t.client.History(ctx, m)
}
func (t *waTransport) DownloadAudio(ctx context.Context, raw []byte, path string) error {
	return t.client.DownloadAudio(ctx, raw, path)
}
func (t *waTransport) AudioReady() bool { return t.Connected() }
func (t *waTransport) Close() error {
	t.once.Do(func() { close(t.stop) })
	<-t.done
	return t.client.Close()
}
