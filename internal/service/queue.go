package service

import (
	"context"
	"sync"
)

const maxWaitingWork = 32

type queueRequest struct {
	ready   chan struct{}
	manual  bool
	granted bool
}

// workQueue serializes work, gives manual requests priority, and cancels waiting
// requests without touching the active job. It owns all scheduling state.
type workQueue struct {
	mu       sync.Mutex
	capacity int
	busy     bool
	waiting  []*queueRequest
}

func (q *workQueue) acquire(ctx context.Context, manual bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := &queueRequest{ready: make(chan struct{}), manual: manual}
	q.mu.Lock()
	capacity := q.capacity
	if capacity <= 0 {
		capacity = maxWaitingWork
	}
	if len(q.waiting) >= capacity {
		q.mu.Unlock()
		return nil, conflict("A fila de trabalho está cheia. Aguarde e tente novamente.")
	}
	q.waiting = append(q.waiting, r)
	q.dispatch()
	q.mu.Unlock()
	select {
	case <-ctx.Done():
		q.mu.Lock()
		if r.granted {
			q.busy = false
		} else {
			for i, candidate := range q.waiting {
				if candidate == r {
					q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
					break
				}
			}
		}
		q.dispatch()
		q.mu.Unlock()
		return nil, ctx.Err()
	case <-r.ready:
	}
	var once sync.Once
	release := func() { once.Do(func() { q.mu.Lock(); q.busy = false; q.dispatch(); q.mu.Unlock() }) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
func (q *workQueue) dispatch() {
	if q.busy || len(q.waiting) == 0 {
		return
	}
	next := 0
	for i, r := range q.waiting {
		if r.manual {
			next = i
			break
		}
	}
	r := q.waiting[next]
	q.waiting = append(q.waiting[:next], q.waiting[next+1:]...)
	q.busy = true
	r.granted = true
	close(r.ready)
}
