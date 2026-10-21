package archive

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"wpp/internal/localization"
)

// Lock is held by the kernel, so a killed process cannot leave a stale lock.
func Lock(dir string) (func(), error) {
	return lockFile(dir, "wpp.lock")
}
func AnalysisLock(dir, group string) (func(), error) {
	return lockFile(dir, "analysis-"+digest([]byte(group))+".lock")
}
func AudioLock(dir string) (func(), error) { return lockFile(dir, "audio-worker.lock") }
func lockFile(dir, name string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(localization.New("archive.another_process_is_using_this_data_directory"), f.Close())
	}
	// Closing the descriptor releases its kernel lock. The callback cannot report errors.
	return func() { _ = f.Close() }, nil
}
