package archive

import (
	"context"
	"errors"
	"time"
	"wpp/internal/localization"
)

type HistorySource interface {
	History(context.Context, Message) ([]Message, error)
}
type HistoryError struct{ Err error }

func (e *HistoryError) Error() string {
	return localization.Text(localization.English, "archive.history_unconfirmed", nil)
}
func (e *HistoryError) Unwrap() error { return e.Err }

type PersistenceError struct{ Err error }

func (e *PersistenceError) Error() string {
	return localization.Text(localization.English, "archive.recovery_persistence_failure", nil)
}
func (e *PersistenceError) Unwrap() error { return e.Err }

type Synchronizer struct {
	Store  *Store
	Source HistorySource
	Delay  time.Duration
}

// Recover uses a recent seed to walk backwards to the persisted checkpoint,
// then the oldest persisted message to extend the archive. It never interprets
// overlap or an empty response as proof that the transcript is complete.
func (s Synchronizer) Recover(ctx context.Context, chat Chat, checkpoint *Message) (err error) {
	attempt, err := s.Store.BeginRecovery(chat.ID, checkpoint, "history.receiving")
	if err != nil {
		return err
	}
	detail := "history.interrupted"
	defer func() {
		if e := s.Store.FinishAttempt(attempt, detail); e != nil {
			err = errors.Join(err, &PersistenceError{Err: e})
		}
	}()
	chats, err := s.Store.Chats()
	if err != nil {
		return err
	}
	canonicalID, err := s.Store.Canonical(chat.ID)
	if err != nil {
		return err
	}
	for _, c := range chats {
		if c.ID == canonicalID {
			chat = c
			break
		}
	}
	if chat.Seed == nil {
		detail = "history.no_reference"
		return nil
	}
	current := *chat.Seed
	current.Sender, err = s.Store.Canonical(current.Sender)
	if err != nil {
		return err
	}
	current.Chat = canonicalID
	if checkpoint != nil {
		copy := *checkpoint
		copy.Sender, err = s.Store.Canonical(copy.Sender)
		if err != nil {
			return err
		}
		copy.Chat = canonicalID
		checkpoint = &copy
	}
	// A history request only retrieves messages BEFORE its anchor. Without a
	// newer seed it cannot bridge downtime; extend the oldest cursor instead.
	if checkpoint != nil && sameKey(current, *checkpoint) {
		_, messages, _, e := s.Store.Snapshot(chat.ID)
		if e != nil {
			return e
		}
		for _, m := range messages {
			if !m.Mutation && len(m.Wire) > 0 {
				current = m
				break
			}
		}
	}
	seen := map[string]bool{anchor(current.ID, keySender(current), current.FromMe): true}
	overlap := false
	for {
		batch, e := s.Source.History(ctx, current)
		if e != nil {
			detail = "history.unavailable"
			if ctx.Err() != nil {
				detail = "history.interrupted"
			}
			return &HistoryError{Err: e}
		}
		// Live history callbacks may have already persisted the batch. Re-ingestion
		// is harmless and keeps fake sources and real sources on the same path.
		if _, e = s.Store.Ingest(batch); e != nil {
			return e
		}
		oldest := older(batch, current)
		for _, m := range batch {
			if checkpoint != nil && sameKey(m, *checkpoint) {
				overlap = true
			}
		}
		if oldest == nil || seen[anchor(oldest.ID, keySender(*oldest), oldest.FromMe)] {
			detail = "history.no_earlier_messages"
			break
		}
		current = *oldest
		seen[anchor(current.ID, keySender(current), current.FromMe)] = true
		if overlap {
			if e = s.Store.ObserveBoundary(attempt); e != nil {
				return e
			}
			detail = "history.overlap"
			break
		}
		if e = pause(ctx, s.Delay); e != nil {
			return e
		}
	}
	// Extend older history independently from the offline recovery boundary.
	if overlap {
		_, messages, _, e := s.Store.Snapshot(chat.ID)
		if e != nil {
			return e
		}
		var oldest *Message
		for _, m := range messages {
			if !m.Mutation && len(m.Wire) > 0 && (oldest == nil || m.Time.Before(oldest.Time)) {
				copy := m
				oldest = &copy
			}
		}
		if oldest != nil {
			current = *oldest
			seen = map[string]bool{anchor(current.ID, keySender(current), current.FromMe): true}
			for {
				batch, e := s.Source.History(ctx, current)
				if e != nil {
					detail = "history.expansion"
					return &HistoryError{Err: e}
				}
				if _, e = s.Store.Ingest(batch); e != nil {
					return e
				}
				previous := older(batch, current)
				if previous == nil || seen[anchor(previous.ID, keySender(*previous), previous.FromMe)] {
					break
				}
				current = *previous
				seen[anchor(current.ID, keySender(current), current.FromMe)] = true
				if e = pause(ctx, s.Delay); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func sameKey(a, b Message) bool {
	return a.ID == b.ID && a.FromMe == b.FromMe && keySender(a) == keySender(b)
}
func older(batch []Message, current Message) *Message {
	var out *Message
	for _, m := range batch {
		if m.Mutation || len(m.Wire) == 0 {
			continue
		}
		if !m.Time.Before(current.Time) && (!m.Time.Equal(current.Time) || sameKey(m, current)) {
			continue
		}
		if out == nil || m.Time.Before(out.Time) || (m.Time.Equal(out.Time) && m.ID < out.ID) {
			copy := m
			out = &copy
		}
	}
	return out
}
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func Checkpoints(s *Store) (map[string]*Message, error) {
	chats, err := s.Chats()
	if err != nil {
		return nil, err
	}
	out := map[string]*Message{}
	for _, c := range chats {
		if !c.Tracked {
			continue
		}
		_, messages, attempts, err := s.Snapshot(c.ID)
		if err != nil {
			return nil, err
		}
		for i := len(messages) - 1; i >= 0; i-- {
			if !messages[i].Mutation && len(messages[i].Wire) > 0 {
				copy := messages[i]
				out[c.ID] = &copy
				break
			}
		}
		// Preserve an unresolved offline boundary over repeated restarts/retries.
		found := false
		for _, a := range attempts {
			if a.BoundaryObserved || a.Anchor == "" {
				continue
			}
			for _, m := range messages {
				if m.ID == a.Anchor && !m.Mutation && len(m.Wire) > 0 && (a.AnchorKey == "" || a.AnchorKey == anchor(m.ID, keySender(m), m.FromMe)) {
					copy := m
					out[c.ID] = &copy
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return out, nil
}

func (s Synchronizer) All(ctx context.Context, checkpoints map[string]*Message, onError func(error)) error {
	chats, err := s.Store.Chats()
	if err != nil {
		return err
	}
	for _, c := range chats {
		if !c.Tracked {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.Recover(ctx, c, checkpoints[c.ID]); err != nil {
			var persistence *PersistenceError
			var history *HistoryError
			if errors.As(err, &persistence) || !errors.As(err, &history) {
				return err
			}
			onError(localization.Errorf("archive.recovery_unconfirmed_for_s", c.ID))
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	return nil
}

func (e *HistoryError) MessageCode() string     { return "archive.history_unconfirmed" }
func (e *PersistenceError) MessageCode() string { return "archive.recovery_persistence_failure" }
