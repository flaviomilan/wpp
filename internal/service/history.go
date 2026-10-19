package service

import (
	"context"
	"sync"
	"time"
)

type historyCoordinator struct {
	mu      sync.Mutex
	queue   workQueue
	running bool
	cancel  context.CancelFunc
	next    time.Time
	waiting map[string]bool
}

func (h *historyCoordinator) request() { h.mu.Lock(); h.next = time.Time{}; h.mu.Unlock() }
func (h *historyCoordinator) pause() {
	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}
	h.mu.Unlock()
}
func (h *historyCoordinator) awaitSeed(id string) {
	h.mu.Lock()
	if h.waiting == nil {
		h.waiting = map[string]bool{}
	}
	h.waiting[id] = true
	h.mu.Unlock()
}
func (h *historyCoordinator) seedObserved(id string) {
	h.mu.Lock()
	if h.waiting[id] {
		delete(h.waiting, id)
		h.next = time.Time{}
	}
	h.mu.Unlock()
}
func (h *historyCoordinator) ready() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.running && !time.Now().Before(h.next)
}
func (h *historyCoordinator) begin(parent context.Context) (context.Context, context.CancelFunc, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running || parent.Err() != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	h.running = true
	h.cancel = cancel
	h.next = time.Now().Add(5 * time.Minute)
	return ctx, cancel, true
}
func (h *historyCoordinator) finish()      { h.mu.Lock(); h.running = false; h.cancel = nil; h.mu.Unlock() }
func (h *historyCoordinator) active() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.running }
