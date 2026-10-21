package service

import (
	"context"
	"errors"
	"wpp/internal/archive"
)

func audioRetryReason(state archive.AudioRetryState) string {
	switch {
	case state.State == "done":
		return "backend.transcription_completed"
	case state.State == "processing":
		return "backend.the_audio_is_already_being_processed"
	case state.Deleted || state.State == "cancelled":
		return "backend.message_deleted_or_replaced"
	case !state.HasMetadata:
		return "backend.sync_the_chat_to_try_retrieving_audio_metadata"
	default:
		return "backend.the_audio_is_waiting_for_processing"
	}
}

// RetryAudio schedules one source through the shared worker without resuming capture.
func (c *Controller) RetryAudio(source string) (archive.Operation, error) {
	c.mu.Lock()
	owner := c.owner
	c.mu.Unlock()
	if !owner {
		return archive.Operation{}, ErrReadOnly
	}
	item, err := c.store.AudioRetryState(source)
	if err != nil {
		return archive.Operation{}, err
	}
	source = item.Source
	if op, ok := c.operations.existing("audio_retry", item.Chat, source); ok {
		return op, nil
	}
	if !item.Tracked {
		return archive.Operation{}, conflict("backend.track_the_chat_before_processing_this_audio")
	}
	if !item.RetryEligible {
		return archive.Operation{}, conflict(audioRetryReason(item))
	}
	return c.submitSource("audio_retry", item.Chat, source, func(ctx context.Context, task *task) (resultErr error) {
		endManual := c.audio.manualRequest()
		defer endManual()
		if err := c.update(task, "running", "waiting_worker", "backend.waiting_for_the_current_audio_to_finish", 1, 0); err != nil {
			return err
		}
		releaseAudio, err := c.audio.queue.acquire(ctx, true)
		if err != nil {
			return err
		}
		defer releaseAudio()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.store.RetryAudio(source); err != nil {
			return &archive.PersistenceError{Err: err}
		}
		defer func() {
			if resultErr != nil {
				if err := c.store.NoteAudioRetryInterrupted(source); err != nil {
					resultErr = errors.Join(resultErr, &archive.PersistenceError{Err: err})
					c.storageFailure(err)
				}
			}
		}()
		c.mu.Lock()
		enabled := c.settings.Transcription
		c.mu.Unlock()
		if !enabled {
			if err := c.store.NoteAudioRetryInterrupted(source); err != nil {
				return &archive.PersistenceError{Err: err}
			}
			if err := c.update(task, "warning", "paused", "backend.audio_requeued_enable_automatic_transcription_and_request_this_audio_again", 1, 0); err != nil {
				return err
			}
			return nil
		}
		if err := c.audio.awaitEngine(ctx); err != nil {
			return err
		}
		c.mu.Lock()
		ready := c.audio.engineAvailable()
		enabled = c.settings.Transcription
		c.mu.Unlock()
		if !ready || !enabled {
			if err := c.store.NoteAudioRetryInterrupted(source); err != nil {
				return &archive.PersistenceError{Err: err}
			}
			if err := c.update(task, "warning", "paused", "backend.audio_requeued_check_the_engine_enable_automatic_transcription_and_try", 1, 0); err != nil {
				return err
			}
			return nil
		}
		if err := c.update(task, "running", "connecting", "backend.connecting_to_process_only_this_audio", 1, 0); err != nil {
			return err
		}
		transport, release, err := c.acquire(ctx, true)
		if err != nil {
			return err
		}
		defer release()
		c.mu.Lock()
		enabled = c.settings.Transcription
		c.mu.Unlock()
		if !enabled {
			if err := c.store.NoteAudioRetryInterrupted(source); err != nil {
				return &archive.PersistenceError{Err: err}
			}
			if err := c.update(task, "warning", "paused", "backend.audio_requeued_enable_automatic_transcription_and_request_this_audio_again", 1, 0); err != nil {
				return err
			}
			return nil
		}
		if err = c.processAudio(ctx, transport, source); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		state, err := c.store.AudioState(source)
		if err != nil {
			return &archive.PersistenceError{Err: err}
		}
		if state != "done" {
			if err := c.update(task, "warning", "finished", "backend.transcription_did_not_complete_check_the_error_on_the_audio", 1, 1); err != nil {
				return err
			}
		} else {
			if err := c.update(task, "running", "finished", "backend.transcription_completed_read_the_text_in_the_audio_queue", 1, 1); err != nil {
				return err
			}
		}
		return nil
	})
}
