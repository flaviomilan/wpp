package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type faultOperations struct {
	mu        sync.Mutex
	calls     int
	failAfter int
	data      map[string]archive.Operation
}

func (s *faultOperations) SaveOperation(op archive.Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls >= s.failAfter {
		return errors.New("simulated disk failure")
	}
	s.data[op.ID] = op
	return nil
}
func (s *faultOperations) Operations() ([]archive.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []archive.Operation{}
	for _, op := range s.data {
		result = append(result, op)
	}
	return result, nil
}
func TestOperationReservationFailureDoesNotAcceptWork(t *testing.T) {
	store := &faultOperations{failAfter: 1, data: map[string]archive.Operation{}}
	m := newOperations(context.Background(), store, func(error) {})
	_, err := m.submit("test", "", "", func(context.Context, *task) error { t.Error("unreserved work executed"); return nil })
	var persistence *archive.PersistenceError
	if !errors.As(err, &persistence) {
		t.Fatal(err)
	}
	requireNoError(t, m.close())
}

func TestOperationRejectsProgressAfterCompletion(t *testing.T) {
	store := &faultOperations{failAfter: 100, data: map[string]archive.Operation{}}
	m := newOperations(context.Background(), store, func(error) {})
	started := make(chan *task, 1)
	op, err := m.submit("test", "", "", func(ctx context.Context, task *task) error {
		started <- task
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	task := <-started
	if err = m.cancel(op.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.close(); err != nil {
		t.Fatal(err)
	}
	if err = m.update(task, "running", "cancelling", "late", 0, 0); err == nil {
		t.Fatal("terminal progress accepted")
	}
	rows, err := m.snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "cancelled" {
		t.Fatal(rows, err)
	}
}

func TestConcurrentCancellationPreservesTerminalState(t *testing.T) {
	for i := 0; i < 200; i++ {
		store := &faultOperations{failAfter: 100, data: map[string]archive.Operation{}}
		m := newOperations(context.Background(), store, func(error) {})
		started := make(chan struct{})
		op, err := m.submit("test", "", "", func(ctx context.Context, _ *task) error { close(started); <-ctx.Done(); return ctx.Err() })
		if err != nil {
			t.Fatal(err)
		}
		<-started
		var callers sync.WaitGroup
		for n := 0; n < 4; n++ {
			callers.Go(func() { _ = m.cancel(op.ID) })
		}
		callers.Wait()
		if err = m.close(); err != nil {
			t.Fatal(err)
		}
		rows, err := m.snapshot()
		if err != nil || len(rows) != 1 || rows[0].State != "cancelled" {
			t.Fatalf("iteration %d: %v %v", i, rows, err)
		}
	}
}

func TestOperationCapacityRejectsBeforePersistenceButAllowsDeduplication(t *testing.T) {
	store := &faultOperations{failAfter: 100, data: map[string]archive.Operation{}}
	m := newOperations(context.Background(), store, func(error) {})
	m.limit = 1
	defer func() { requireNoError(t, m.close()) }()
	started := make(chan struct{})
	first, err := m.submit("sync", "group", "", func(ctx context.Context, _ *task) error { close(started); <-ctx.Done(); return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	duplicate, err := m.submit("sync", "group", "", func(context.Context, *task) error { t.Error("duplicate executed"); return nil })
	if err != nil || duplicate.ID != first.ID {
		t.Fatal(duplicate, err)
	}
	if _, err = m.submit("sync", "other", "", func(context.Context, *task) error { t.Error("over-capacity work executed"); return nil }); err == nil {
		t.Fatal("capacity ignored")
	}
	store.mu.Lock()
	count := len(store.data)
	store.mu.Unlock()
	if count != 1 {
		t.Fatal("rejection left persisted operation", count)
	}
}

func TestOperationPreservesStorageFailureAlongsideCancellationAndHistory(t *testing.T) {
	store := &faultOperations{failAfter: 100, data: map[string]archive.Operation{}}
	fault := make(chan error, 1)
	m := newOperations(context.Background(), store, func(err error) { fault <- err })
	op, err := m.submit("sync", "group@g.us", "", func(_ context.Context, task *task) error {
		task.cancel()
		return errors.Join(context.Canceled, &archive.HistoryError{Err: errors.New("PRIVATE history")}, &archive.PersistenceError{Err: errors.New("PRIVATE disk")})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !persistenceFailure(<-fault) {
		t.Fatal("storage fault was discarded")
	}
	wait(t, func() bool {
		rows, _ := m.snapshot()
		return len(rows) == 1 && rows[0].State == "failed"
	})
	rows, err := m.snapshot()
	if err != nil || rows[0].ID != op.ID || rows[0].State != "failed" || rows[0].Detail == "PRIVATE disk" {
		t.Fatal(rows, err)
	}
	if err = m.close(); err != nil {
		t.Fatal(err)
	}
}
func TestOperationProgressFailureRemainsVisibleAndCancelsWork(t *testing.T) {
	store := &faultOperations{failAfter: 2, data: map[string]archive.Operation{}}
	m := newOperations(context.Background(), store, func(error) {})
	op, err := m.submit("test", "", "", func(context.Context, *task) error {
		t.Error("work executed after failed mandatory progress")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.close(); err == nil {
		t.Fatal("shutdown discarded persistence failure")
	}
	rows, err := m.snapshot()
	if err != nil || len(rows) != 1 || rows[0].ID != op.ID || rows[0].State != "failed" {
		t.Fatal(rows, err)
	}
}
func TestQueueCancelsWaitingWorkAndPrioritizesManualRequests(t *testing.T) {
	var q workQueue
	release, err := q.acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	automatic := make(chan func(), 1)
	manual := make(chan func(), 1)
	go func() { r, _ := q.acquire(context.Background(), false); automatic <- r }()
	// Observe queue admission through a test-only barrier before admitting manual work.
	wait(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.waiting) == 1 })
	go func() { r, _ := q.acquire(context.Background(), true); manual <- r }()
	wait(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.waiting) == 2 })
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() { _, err := q.acquire(ctx, true); cancelled <- err }()
	cancel()
	if err = <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	manualRelease := <-manual
	select {
	case <-automatic:
		t.Fatal("automatic work overtook manual work")
	default:
	}
	manualRelease()
	automaticRelease := <-automatic
	automaticRelease()
}

func TestLocalizedOperationMetadata(t *testing.T) {
	op := archive.Operation{State: "running", Detail: "backend.processing_audio_completed_of_total", Total: 3, Completed: 1}
	prepareOperation(&op)
	if op.Detail != "Processing audio: 1 of 3." || op.DetailCode == "" {
		t.Fatal(op)
	}
	if localized := localization.Text("pt-BR", op.DetailCode, op.DetailParameters); localized != "Processando áudios: 1 de 3." {
		t.Fatal(localized)
	}
	// Updating presentation repeatedly preserves identifiers and named counts.
	prepareOperation(&op)
	if op.DetailParameters["completed"] != "1" || op.DetailParameters["total"] != "3" {
		t.Fatal(op)
	}
}

func TestApplicationErrorLocalizedIdentity(t *testing.T) {
	err := invalid("backend.invalid_identifier")
	if got := localization.Render(err, "pt-BR"); got != "Identificador inválido." {
		t.Fatal(got)
	}
	var application *ApplicationError
	if !errors.As(err, &application) || application.MessageCode() != "backend.invalid_identifier" || application.Conflict {
		t.Fatal("lost error contract", err)
	}
}
