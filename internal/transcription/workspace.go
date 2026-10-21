package transcription

import (
	"errors"
	"os"
	"path/filepath"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type Workspace struct {
	Root   string
	unlock func()
}

func OpenWorkspace(dir string, store *archive.Store) (*Workspace, error) {
	unlock, err := archive.AudioLock(dir)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "tmp-audio")
	fail := func(err error) (*Workspace, error) { unlock(); return nil, err }
	if info, e := os.Lstat(root); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fail(localization.New("backend.invalid_audio_temporary_directory"))
	}
	if err = os.RemoveAll(root); err != nil {
		return fail(err)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return fail(err)
	}
	if err = store.RecoverAudioWorker(); err != nil {
		return fail(errors.Join(&archive.PersistenceError{Err: err}, os.RemoveAll(root)))
	}
	return &Workspace{Root: root, unlock: unlock}, nil
}
func (w *Workspace) Close() error { err := os.RemoveAll(w.Root); w.unlock(); return err }
