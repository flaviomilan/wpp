package webui

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Real HTTP over an in-memory pipe requires no listener permissions or network.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }

func TestHTTPShutdownWaitsForHandlers(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		force bool
	}{{"graceful", false}, {"timeout", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			listener := &pipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{})}
			client, connection := net.Pipe()
			closeTest(t, client)
			listener.connections <- connection
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				if scenario.force {
					<-r.Context().Done()
					close(cancelled)
				}
				<-release
				w.WriteHeader(http.StatusNoContent)
			})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			timeout := time.Second
			if scenario.force {
				timeout = 20 * time.Millisecond
			}
			go func() { finished <- serveHTTP(ctx, server, listener, timeout) }()
			clientDone := make(chan error, 1)
			go func() {
				_, writeErr := io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
				_, readErr := io.Copy(io.Discard, client)
				clientDone <- errors.Join(writeErr, readErr)
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("handler did not start")
			}
			cancel()
			<-listener.closed
			if scenario.force {
				select {
				case <-cancelled:
				case <-time.After(2 * time.Second):
					t.Fatal("timeout did not cancel network handler")
				}
			}
			select {
			case err := <-finished:
				t.Fatal("returned before handler cleanup", err)
			default:
			}
			unblock()
			select {
			case err := <-finished:
				if scenario.force && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("timeout discarded", err)
				}
				if !scenario.force && err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown did not finish")
			}
			select {
			case err := <-clientDone:
				if !scenario.force && err != nil {
					t.Error(err)
				}
			case <-time.After(time.Second):
				t.Fatal("client goroutine did not exit")
			}
		})
	}
}

func TestServeControlledUsesLoopbackListenerAndWaitsForShutdown(t *testing.T) {
	for _, tc := range []struct {
		name, address string
		fail          bool
	}{{"valid", "127.0.0.1:8080", false}, {"invalid", "bad", false}, {"public", "0.0.0.0:8080", false}, {"listen_failure", "127.0.0.1:8080", true}} {
		t.Run(tc.name, func(t *testing.T) {
			listener := &pipeListener{connections: make(chan net.Conn), closed: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan string, 1)
			done := make(chan error, 1)
			sentinel := errors.New("listener unavailable")
			go func() {
				done <- serveControlled(ctx, nil, tc.address, func(url string) { ready <- url }, time.UTC, nil, func(network, address string) (net.Listener, error) {
					if network != "tcp" || address != tc.address {
						t.Error("wrong listener arguments")
					}
					if tc.fail {
						return nil, sentinel
					}
					return listener, nil
				})
			}()
			if tc.name == "valid" {
				select {
				case url := <-ready:
					if url != "http://127.0.0.1:8080" {
						t.Fatal(url)
					}
				case <-time.After(time.Second):
					t.Fatal("not ready")
				}
				cancel()
			}
			select {
			case err := <-done:
				if tc.name == "valid" && err != nil {
					t.Fatal(err)
				}
				if tc.name != "valid" && err == nil {
					t.Fatal("invalid listener accepted")
				}
				if tc.fail && !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("server did not stop")
			}
		})
	}
}
