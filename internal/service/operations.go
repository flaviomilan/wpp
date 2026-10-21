package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sort"
	"strconv"
	"sync"
	"time"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type operationStore interface {
	SaveOperation(archive.Operation) error
	Operations() ([]archive.Operation, error)
}
type task struct {
	op      archive.Operation
	cancel  context.CancelFunc
	failure error
}

const maxActiveOperations = 24

type operationManager struct {
	mu          sync.Mutex
	wg          sync.WaitGroup
	ctx         context.Context
	store       operationStore
	active      map[string]*task
	unsaved     map[string]archive.Operation
	limit       int
	closed      bool
	shutdownErr error
	fault       func(error)
}

func newOperations(ctx context.Context, store operationStore, fault func(error)) *operationManager {
	return &operationManager{ctx: ctx, store: store, active: map[string]*task{}, unsaved: map[string]archive.Operation{}, fault: fault, limit: maxActiveOperations}
}
func (m *operationManager) existing(kind, chat, source string) (archive.Operation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.active {
		if t.op.Kind == kind && t.op.Chat == chat && t.op.Source == source {
			return t.op, true
		}
	}
	return archive.Operation{}, false
}
func (m *operationManager) submit(kind, chat, source string, fn func(context.Context, *task) error) (archive.Operation, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return archive.Operation{}, conflict("backend.the_panel_is_shutting_down")
	}
	for _, t := range m.active {
		if t.op.Kind == kind && t.op.Chat == chat && t.op.Source == source {
			op := t.op
			m.mu.Unlock()
			return op, nil
		}
	}
	if len(m.active) >= m.limit {
		m.mu.Unlock()
		return archive.Operation{}, conflict("backend.the_operation_queue_is_full_wait_for_an_operation_to")
	}
	ctx, cancel := context.WithCancel(m.ctx)
	now := time.Now().UTC()
	t := &task{op: archive.Operation{ID: uuid.NewString(), Kind: kind, Chat: chat, Source: source, State: "queued", Phase: "queued", Detail: "backend.waiting_to_run", Started: now, Updated: now}, cancel: cancel}
	prepareOperation(&t.op)
	if err := m.store.SaveOperation(t.op); err != nil {
		cancel()
		m.mu.Unlock()
		m.fault(err)
		return archive.Operation{}, &archive.PersistenceError{Err: err}
	}
	m.active[t.op.ID] = t
	op := t.op
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer cancel()
		err := m.update(t, "running", "starting", "backend.preparing_the_operation", 0, 0)
		if err == nil {
			err = fn(ctx, t)
		}
		if persistenceFailure(err) {
			m.fault(err)
		}
		m.mu.Lock()
		switch {
		case t.failure != nil:
			t.op.State = "failed"
			t.op.Detail = "backend.failed_to_save_the_operation_check_storage"
		case persistenceFailure(err):
			t.op.State = "failed"
			t.op.Detail = operationError(err)
		case ctx.Err() != nil:
			t.op.State = "cancelled"
			t.op.Detail = "backend.operation_cancelled_received_data_was_preserved"
		case err != nil:
			t.op.State = "failed"
			t.op.Detail = operationError(err)
		case t.op.State != "warning":
			t.op.State = "succeeded"
		}
		t.op.Updated = time.Now().UTC()
		saveErr := m.writeLocked(t)
		delete(m.active, t.op.ID)
		m.mu.Unlock()
		if saveErr != nil {
			m.fault(saveErr)
		}
	}()
	return op, nil
}
func (m *operationManager) writeLocked(t *task) error {
	prepareOperation(&t.op)
	err := m.store.SaveOperation(t.op)
	if err != nil {
		t.failure = errors.Join(t.failure, &archive.PersistenceError{Err: err})
		m.shutdownErr = errors.Join(m.shutdownErr, t.failure)
		t.op.State = "failed"
		t.op.Detail = "backend.failed_to_save_the_operation_check_storage"
		prepareOperation(&t.op)
		m.unsaved[t.op.ID] = t.op
		t.cancel()
	} else {
		delete(m.unsaved, t.op.ID)
	}
	return err
}
func (m *operationManager) update(t *task, state, phase, detail string, total, completed int) error {
	m.mu.Lock()
	err := m.updateLocked(t, state, phase, detail, total, completed)
	m.mu.Unlock()
	if persistenceFailure(err) {
		m.fault(err)
	}
	return err
}

func (m *operationManager) updateLocked(t *task, state, phase, detail string, total, completed int) error {
	if m.active[t.op.ID] != t {
		return conflict("backend.the_operation_has_finished_or_does_not_exist")
	}
	if t.failure != nil {
		return t.failure
	}
	if t.op.Phase == "cancelling" && phase != "cancelling" {
		return context.Canceled
	}
	t.op.State = state
	t.op.Phase = phase
	t.op.Detail = detail
	t.op.DetailCode = ""
	t.op.DetailParameters = nil
	t.op.Total = total
	t.op.Completed = completed
	t.op.Updated = time.Now().UTC()
	if err := m.writeLocked(t); err != nil {
		return &archive.PersistenceError{Err: err}
	}
	return nil
}

func (m *operationManager) cancel(id string) error {
	m.mu.Lock()
	t := m.active[id]
	if t == nil {
		m.mu.Unlock()
		return conflict("backend.the_operation_has_finished_or_does_not_exist")
	}
	t.cancel()
	err := m.updateLocked(t, "running", "cancelling", "backend.cancelling_the_operation_preserving_received_data", t.op.Total, t.op.Completed)
	m.mu.Unlock()
	if persistenceFailure(err) {
		m.fault(err)
	}
	return err
}
func (m *operationManager) cancelChat(chat string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.active {
		if t.op.Chat == chat {
			t.cancel()
		}
	}
}
func (m *operationManager) progress(source, phase string) error {
	m.mu.Lock()
	var found *task
	for _, t := range m.active {
		if t.op.Kind == "audio_retry" && t.op.Source == source {
			found = t
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return nil
	}
	return m.update(found, "running", phase, "backend.processing_the_selected_audio", 1, 0)
}
func (m *operationManager) snapshot() ([]archive.Operation, error) {
	ops, err := m.store.Operations()
	m.mu.Lock()
	merged := map[string]archive.Operation{}
	for _, op := range ops {
		merged[op.ID] = op
	}
	for id, t := range m.active {
		merged[id] = t.op
	}
	for id, op := range m.unsaved {
		merged[id] = op
	}
	m.mu.Unlock()
	result := make([]archive.Operation, 0, len(merged))
	for _, op := range merged {
		result = append(result, op)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Updated.After(result[j].Updated) })
	if len(result) > 30 {
		result = result[:30]
	}
	return result, err
}
func (m *operationManager) close() error {
	m.mu.Lock()
	m.closed = true
	for _, t := range m.active {
		t.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdownErr
}

func prepareOperation(op *archive.Operation) {
	if localization.Known(op.Detail) {
		op.DetailCode = op.Detail
		op.DetailParameters = map[string]string{"total": strconv.Itoa(op.Total), "completed": strconv.Itoa(op.Completed)}
		op.Detail = localization.Text(localization.English, op.DetailCode, op.DetailParameters)
	}
}
