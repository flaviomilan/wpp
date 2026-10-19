package webui

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Admission stops before waiting, so a late request cannot race WaitGroup.Wait.
type requestLifecycle struct {
	mu      sync.Mutex
	closing bool
	active  sync.WaitGroup
}

func (l *requestLifecycle) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			http.Error(w, "O painel está encerrando.", http.StatusServiceUnavailable)
			return
		}
		l.active.Add(1)
		l.mu.Unlock()
		defer l.active.Done()
		next.ServeHTTP(w, r)
	})
}

func (l *requestLifecycle) stop() {
	l.mu.Lock()
	l.closing = true
	l.mu.Unlock()
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) error {
	var requests requestLifecycle
	server.Handler = requests.wrap(server.Handler)
	finished := make(chan struct{})
	shutdown := make(chan error, 1)
	go func() {
		var err error
		select {
		case <-ctx.Done():
			requests.stop()
			stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			err = server.Shutdown(stop)
			cancel()
			if err != nil {
				err = errors.Join(err, server.Close())
			}
		case <-finished:
			requests.stop()
			err = server.Close()
		}
		// Close cancels network handlers after a timeout; their cleanup must still
		// finish before the caller closes the controller and archive.
		requests.active.Wait()
		shutdown <- err
	}()
	err := server.Serve(listener)
	close(finished)
	shutdownErr := <-shutdown
	if errors.Is(err, http.ErrServerClosed) {
		return shutdownErr
	}
	return errors.Join(err, shutdownErr)
}
