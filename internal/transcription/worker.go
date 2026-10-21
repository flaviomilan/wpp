package transcription

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type Downloader interface {
	DownloadAudio(context.Context, []byte, string) error
}
type Engine interface {
	Transcribe(context.Context, string, string) (archive.Transcript, error)
}
type Worker struct {
	Store    *archive.Store
	Download Downloader
	Engine   Engine
	Timeout  time.Duration
	Source   string
	Phase    func(archive.AudioJob, string)
}

func (w Worker) ProcessOne(ctx context.Context, root string) (worked bool, resultErr error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if ready, ok := w.Download.(interface{ AudioReady() bool }); ok && !ready.AudioReady() {
		return false, nil
	}
	if w.Timeout <= 0 {
		w.Timeout = 30 * time.Minute
	}
	job, err := w.Store.ClaimAudioSource(time.Now(), w.Timeout+time.Minute, w.Source)
	if err != nil {
		return false, &archive.PersistenceError{Err: err}
	}
	if job == nil {
		return false, nil
	}
	dir, dirErr := os.MkdirTemp(root, "job-")
	if dirErr != nil {
		return true, w.release(*job, "backend.failed_to_prepare_temporary_directory", ctx.Err() != nil)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			resultErr = errors.Join(resultErr, localization.New("backend.failed_to_clean_up_temporary_audio"), err)
		}
	}()
	phaseErr := w.phase(*job, "downloading")
	if phaseErr != nil {
		return true, errors.Join(phaseErr, w.release(*job, "backend.failed_to_save_processing_phase", true))
	}
	transcript, detail, err := w.process(ctx, dir, *job)
	if err == nil && ctx.Err() != nil {
		err, detail = ctx.Err(), "backend.transcription_interrupted"
	}
	if err == nil {
		err = w.phase(*job, "saving")
		if err == nil && ctx.Err() != nil {
			return true, w.release(*job, "backend.transcription_interrupted", true)
		}
		if err == nil {
			err = w.Store.CompleteAudio(*job, transcript)
		}
		if err != nil {
			err = &archive.PersistenceError{Err: err}
		}
		detail = "backend.failed_to_save_transcription"
	}
	if err != nil {
		if e := w.release(*job, detail, ctx.Err() != nil || errors.Is(err, archive.ErrAudioOffline)); e != nil {
			return true, errors.Join(err, &archive.PersistenceError{Err: e})
		}
	}
	if persistenceFailure(err) {
		return true, err
	}
	return true, nil
}
func (w Worker) release(job archive.AudioJob, detail string, interrupted bool) error {
	if err := w.Store.ReleaseAudio(job, detail, interrupted); err != nil {
		return &archive.PersistenceError{Err: err}
	}
	return nil
}
func (w Worker) phase(job archive.AudioJob, phase string) error {
	if err := w.Store.SetAudioPhase(job, phase); err != nil {
		return &archive.PersistenceError{Err: err}
	}
	if w.Phase != nil {
		w.Phase(job, phase)
	}
	return nil
}
func (w Worker) process(ctx context.Context, dir string, job archive.AudioJob) (archive.Transcript, string, error) {
	var result archive.Transcript
	if ctx.Err() != nil {
		return result, "backend.transcription_interrupted", ctx.Err()
	}
	processing, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	audio := filepath.Join(dir, "original")
	download, cancelDownload := context.WithTimeout(processing, 2*time.Minute)
	err := w.Download.DownloadAudio(download, job.Metadata, audio)
	cancelDownload()
	if err != nil {
		return result, "backend.download_unavailable_or_interrupted", err
	}
	info, err := os.Stat(audio)
	if err != nil || info.Size() > 64*1024*1024 {
		return result, "backend.invalid_audio_or_larger_than_64_mib", localization.New("backend.invalid_file")
	}
	if err = w.phase(job, "transcribing"); err != nil {
		return result, "backend.failed_to_save_processing_phase", err
	}
	if ctx.Err() != nil {
		return result, "backend.transcription_interrupted", ctx.Err()
	}
	if engine, ok := w.Engine.(interface {
		TranscribePhased(context.Context, string, string, func(string)) (archive.Transcript, error)
	}); ok {
		var phaseErr error
		result, err = engine.TranscribePhased(processing, audio, dir, func(phase string) {
			if phaseErr == nil {
				phaseErr = w.phase(job, phase)
				if phaseErr != nil {
					cancel()
				}
			}
		})
		err = errors.Join(err, phaseErr)
	} else {
		result, err = w.Engine.Transcribe(processing, audio, dir)
	}
	if err != nil {
		return result, "backend.transcription_failed_timed_out_or_did_not_recognize_speech", err
	}
	return result, "", nil
}

func persistenceFailure(err error) bool {
	var failure *archive.PersistenceError
	return errors.As(err, &failure)
}
