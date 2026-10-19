package service

import (
	"context"
	"errors"
	"wpp/internal/archive"
)

func audioRetryReason(state archive.AudioRetryState) string {
	switch {
	case state.State == "done":
		return "Transcrição concluída."
	case state.State == "processing":
		return "O áudio já está em processamento."
	case state.Deleted || state.State == "cancelled":
		return "Mensagem excluída ou substituída."
	case !state.HasMetadata:
		return "Sincronize a conversa para tentar recuperar os metadados do áudio."
	default:
		return "O áudio está aguardando processamento."
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
		return archive.Operation{}, conflict("Acompanhe a conversa antes de processar este áudio.")
	}
	if !item.RetryEligible {
		return archive.Operation{}, conflict(audioRetryReason(item))
	}
	return c.submitSource("audio_retry", item.Chat, source, func(ctx context.Context, task *task) (resultErr error) {
		endManual := c.audio.manualRequest()
		defer endManual()
		if err := c.update(task, "running", "waiting_worker", "Aguardando o áudio em andamento terminar.", 1, 0); err != nil {
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
			if err := c.update(task, "warning", "paused", "Áudio reenfileirado. Ative Transcrição automática e solicite novamente este áudio.", 1, 0); err != nil {
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
			if err := c.update(task, "warning", "paused", "Áudio reenfileirado. Confira o motor, ative Transcrição automática e tente novamente.", 1, 0); err != nil {
				return err
			}
			return nil
		}
		if err := c.update(task, "running", "connecting", "Conectando para processar somente este áudio.", 1, 0); err != nil {
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
			if err := c.update(task, "warning", "paused", "Áudio reenfileirado. Ative Transcrição automática e solicite novamente este áudio.", 1, 0); err != nil {
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
			if err := c.update(task, "warning", "finished", "A tentativa não concluiu a transcrição. Confira o erro no cartão do áudio.", 1, 1); err != nil {
				return err
			}
		} else {
			if err := c.update(task, "running", "finished", "Transcrição concluída. Confira o texto na fila de áudios.", 1, 1); err != nil {
				return err
			}
		}
		return nil
	})
}
