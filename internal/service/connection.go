package service

import (
	"context"
	"sync"
	"wpp/internal/archive"
)

type connectionCoordinator struct {
	mu              sync.Mutex
	gate            workQueue
	wg              sync.WaitGroup
	ctx             context.Context
	factory         Factory
	dir             string
	store           *archive.Store
	event           func(Event)
	client          Transport
	refs            int
	linked, blocked bool
	status, detail  string
	qr              []byte
}

func newConnection(ctx context.Context, opts Options, store *archive.Store, event func(Event)) *connectionCoordinator {
	return &connectionCoordinator{ctx: ctx, factory: opts.Factory, dir: opts.DataDir, store: store, event: event, status: "disconnected"}
}
func (c *connectionCoordinator) open() error {
	t, err := c.factory(c.ctx, c.dir, c.store)
	if err != nil {
		return invalid("backend.could_not_open_the_local_session")
	}
	c.mu.Lock()
	c.client = t
	c.linked = t.Session()
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-c.ctx.Done():
				return
			case event, ok := <-t.Events():
				if !ok {
					return
				}
				c.mu.Lock()
				if c.client != t {
					c.mu.Unlock()
					return
				}
				switch event.Kind {
				case "online":
					c.status = "connected"
					c.linked = true
					c.detail = ""
					c.blocked = false
				case "offline":
					c.status = "reconnecting"
				case "needs_login", "error":
					c.status = event.Kind
					c.blocked = true
					if event.Kind == "needs_login" {
						c.linked = false
					}
				}
				c.mu.Unlock()
				c.event(event)
			}
		}
	}()
	return nil
}
func (c *connectionCoordinator) acquire(ctx context.Context, connect bool) (Transport, func(), error) {
	releaseGate, err := c.gate.acquire(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	defer releaseGate()
	c.mu.Lock()
	t, blocked, refs := c.client, c.blocked, c.refs
	if blocked && connect {
		c.mu.Unlock()
		return nil, nil, invalid("backend.link_your_account_again_to_continue")
	}
	if !connect && blocked && refs == 0 && t != nil {
		c.client = nil
		c.blocked = false
	}
	c.mu.Unlock()
	if !connect && blocked && refs == 0 && t != nil {
		if err = t.Close(); err != nil {
			return nil, nil, err
		}
		t = nil
	}
	if t == nil {
		if err = c.open(); err != nil {
			return nil, nil, err
		}
		c.mu.Lock()
		t = c.client
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.refs++
	if connect {
		c.status = "connecting"
	}
	c.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { c.mu.Lock(); c.refs--; c.mu.Unlock() }) }
	if connect {
		if err = t.Connect(ctx); err != nil {
			release()
			c.mu.Lock()
			c.detail = err.Error()
			switch {
			case ctx.Err() != nil:
				c.status = "disconnected"
				c.detail = ""
			case !t.Session():
				c.status = "needs_login"
				c.blocked = true
			default:
				c.status = "error"
			}
			c.mu.Unlock()
			return nil, nil, err
		}
		c.markConnected()
	}
	return t, release, nil
}
func (c *connectionCoordinator) markConnected() {
	c.mu.Lock()
	c.status = "connected"
	c.detail = ""
	c.linked = true
	c.blocked = false
	c.mu.Unlock()
}
func (c *connectionCoordinator) connected() bool {
	c.mu.Lock()
	t := c.client
	c.mu.Unlock()
	return t != nil && t.Connected()
}
func (c *connectionCoordinator) idle(keep bool) error {
	if keep {
		return nil
	}
	release, err := c.gate.acquire(c.ctx, false)
	if err != nil {
		return err
	}
	defer release()
	c.mu.Lock()
	if c.refs != 0 || c.client == nil {
		c.mu.Unlock()
		return nil
	}
	t := c.client
	c.client = nil
	c.mu.Unlock()
	err = t.Close()
	c.mu.Lock()
	if !c.blocked {
		c.status = "disconnected"
	}
	c.mu.Unlock()
	return err
}
func (c *connectionCoordinator) snapshot() (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.detail, c.linked
}
func (c *connectionCoordinator) setQR(raw []byte) {
	c.mu.Lock()
	c.qr = append([]byte(nil), raw...)
	c.mu.Unlock()
}
func (c *connectionCoordinator) QR() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.qr...)
}
func (c *connectionCoordinator) close() error {
	c.wg.Wait()
	release, err := c.gate.acquire(context.Background(), true)
	if err != nil {
		return err
	}
	defer release()
	c.mu.Lock()
	t := c.client
	c.client = nil
	c.mu.Unlock()
	if t != nil {
		return t.Close()
	}
	return nil
}
