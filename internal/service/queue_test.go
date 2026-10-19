package service

import (
	"context"
	"testing"
	"testing/synctest"
)

func TestQueueCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := workQueue{capacity: 1}
		release, err := q.acquire(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		waiting := make(chan func(), 1)
		go func() {
			r, err := q.acquire(context.Background(), true)
			if err != nil {
				t.Error(err)
			}
			waiting <- r
		}()
		synctest.Wait()
		if _, err = q.acquire(context.Background(), true); err == nil {
			t.Fatal("queue limit ignored")
		}
		release()
		(<-waiting)()
		release, err = q.acquire(context.Background(), false)
		if err != nil {
			t.Fatal("capacity not released", err)
		}
		release()
	})
}
