package transcription

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"wpp/internal/archive"
)

type downloadFunc func(context.Context, []byte, string) error

func (f downloadFunc) DownloadAudio(ctx context.Context, raw []byte, path string) error {
	return f(ctx, raw, path)
}

type offlineDownload struct{ fakeDownload }

func (offlineDownload) AudioReady() bool { return false }

type phasedTestEngine struct{}

func (phasedTestEngine) Transcribe(context.Context, string, string) (archive.Transcript, error) {
	return archive.Transcript{}, errors.New("phased method required")
}
func (phasedTestEngine) TranscribePhased(_ context.Context, _, _ string, phase func(string)) (archive.Transcript, error) {
	phase("converting")
	phase("transcribing")
	return archive.Transcript{Text: "Ação concluída", Language: "pt"}, nil
}

func audioStore(t *testing.T) (*archive.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { requireNoError(t, store.Close()) })
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "group@g.us", ID: "audio", Sender: "person@lid", Group: true, Kind: "áudio", AudioMetadata: []byte("fixture")}}); err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func TestWorkspaceRejectsUnsafeRootAndReleasesLockOnFailure(t *testing.T) {
	for _, scenario := range []string{"file", "symlink", "closed-store", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			store, dir := audioStore(t)
			root := filepath.Join(dir, "tmp-audio")
			switch scenario {
			case "file":
				if err := os.WriteFile(root, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(t.TempDir(), root); err != nil {
					t.Fatal(err)
				}
			case "closed-store":
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			case "locked":
				unlock, err := archive.AudioLock(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}
			if workspace, err := OpenWorkspace(dir, store); err == nil || workspace != nil {
				t.Fatal("invalid workspace accepted")
			}
			if scenario != "locked" {
				unlock, err := archive.AudioLock(dir)
				if err != nil {
					t.Fatal("lock leaked", err)
				}
				unlock()
			}
		})
	}
}

func TestWorkerPreconditionsAndDownloadFailures(t *testing.T) {
	for _, scenario := range []string{"cancel", "offline", "root", "download", "missing", "oversize", "disconnected", "phased", "phase-cancel", "phase-failure"} {
		t.Run(scenario, func(t *testing.T) {
			store, _ := audioStore(t)
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			worker := Worker{Store: store, Download: fakeDownload{}, Engine: phasedTestEngine{}}
			switch scenario {
			case "cancel":
				cancel()
			case "offline":
				worker.Download = offlineDownload{}
			case "root":
				root = filepath.Join(root, "absent")
			case "download":
				worker.Download = downloadFunc(func(context.Context, []byte, string) error { return errors.New("private download") })
			case "missing":
				worker.Download = downloadFunc(func(context.Context, []byte, string) error { return nil })
			case "oversize":
				worker.Download = downloadFunc(func(_ context.Context, _ []byte, path string) error {
					file, err := os.Create(path)
					if err != nil {
						return err
					}
					defer func() { requireNoError(t, file.Close()) }()
					return file.Truncate(64*1024*1024 + 1)
				})
			case "disconnected":
				worker.Download = downloadFunc(func(context.Context, []byte, string) error { return archive.ErrAudioOffline })
			case "phase-cancel":
				worker.Phase = func(_ archive.AudioJob, phase string) {
					if phase == "transcribing" {
						cancel()
					}
				}
			case "phase-failure":
				worker.Phase = func(_ archive.AudioJob, phase string) {
					if phase == "converting" {
						if err := store.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			worked, err := worker.ProcessOne(ctx, root)
			if scenario == "cancel" {
				if worked || !errors.Is(err, context.Canceled) {
					t.Fatal(worked, err)
				}
				return
			}
			if scenario == "offline" {
				if worked || err != nil {
					t.Fatal(worked, err)
				}
				return
			}
			if scenario == "phase-failure" {
				if !worked || !persistenceFailure(err) {
					t.Fatal(worked, err)
				}
				return
			}
			if !worked || err != nil {
				t.Fatal(worked, err)
			}
			jobs, err := store.AudioJobs(0, 50)
			if err != nil || len(jobs) != 1 {
				t.Fatal(jobs, err)
			}
			job := jobs[0]
			if scenario == "phased" {
				if job.State != "done" {
					t.Fatal(job)
				}
				if worked, err = worker.ProcessOne(ctx, root); worked || err != nil {
					t.Fatal(worked, err)
				}
			} else {
				if job.State != "pending" || job.Error == "" || job.Error == "private download" {
					t.Fatal(job)
				}
				wantAttempts := 1
				if scenario == "disconnected" || scenario == "phase-cancel" {
					wantAttempts = 0
				}
				if job.Attempts != wantAttempts {
					t.Fatal(job)
				}
			}
		})
	}
}

func TestLanguageAutoAndVersionLength(t *testing.T) {
	if !ValidLanguage("auto") || ValidLanguage("PT") {
		t.Fatal("language rules changed")
	}
	cfg := localConfig(t)
	cfg.Threads = 4
	cfg.Whisper = executable(t, "whisper", "printf '%0300d' 0\n")
	engine, err := NewLocal(context.Background(), cfg)
	if err != nil || len(engine.Version) != 256 {
		t.Fatal(engine, err)
	}
}

func TestWorkspaceRecoversLeaseCleansStaleAudioAndReleasesLock(t *testing.T) {
	store, dir := audioStore(t)
	job, err := store.ClaimAudio(time.Now(), time.Minute)
	if err != nil || job == nil {
		t.Fatal(job, err)
	}
	root := filepath.Join(dir, "tmp-audio")
	requireNoError(t, os.Mkdir(root, 0700))
	requireNoError(t, os.WriteFile(filepath.Join(root, "stale-audio"), []byte("temporary"), 0600))
	workspace, err := OpenWorkspace(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("stale audio retained", files, err)
	}
	jobs, err := store.AudioJobs(0, 50)
	if err != nil || len(jobs) != 1 || jobs[0].State != "pending" {
		t.Fatal("lease not recovered", jobs, err)
	}
	requireNoError(t, workspace.Close())
	if _, err = os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("workspace retained after close", err)
	}
	unlock, err := archive.AudioLock(dir)
	if err != nil {
		t.Fatal("workspace lock leaked", err)
	}
	unlock()
}
