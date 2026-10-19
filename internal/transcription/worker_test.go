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

type fakeDownload struct{}

func (fakeDownload) DownloadAudio(_ context.Context, _ []byte, path string) error {
	return os.WriteFile(path, []byte("mock audio"), 0600)
}

type closingDownload struct{ store *archive.Store }

func (d closingDownload) DownloadAudio(_ context.Context, _ []byte, path string) error {
	if err := os.WriteFile(path, []byte("mock audio"), 0600); err != nil {
		return err
	}
	return d.store.Close()
}

func TestWorkerSurfacesPhasePersistenceFailures(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "group@g.us", ID: "audio", Sender: "123@lid", Group: true, Kind: "áudio", Time: time.Now(), AudioMetadata: []byte("test")}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	worker := Worker{Store: store, Download: closingDownload{store}, Engine: fakeEngine{}}
	worked, err := worker.ProcessOne(context.Background(), root)
	if !worked || !persistenceFailure(err) {
		t.Fatal(worked, err)
	}
	files, readErr := os.ReadDir(root)
	if readErr != nil || len(files) != 0 {
		t.Fatal("temporary audio retained", files, readErr)
	}
	worked, err = worker.ProcessOne(context.Background(), root)
	if worked || !persistenceFailure(err) {
		t.Fatal("claim failure discarded", worked, err)
	}
}

type fakeEngine struct {
	ignoreCancellation bool
	fail               bool
	cancel             context.CancelFunc
}

func (f fakeEngine) Transcribe(ctx context.Context, _ string, dir string) (archive.Transcript, error) {
	if err := os.WriteFile(filepath.Join(dir, "audio.wav"), []byte("mock converted"), 0600); err != nil {
		return archive.Transcript{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte("mock text"), 0600); err != nil {
		return archive.Transcript{}, err
	}
	if f.cancel != nil {
		f.cancel()
		if !f.ignoreCancellation {
			return archive.Transcript{}, ctx.Err()
		}
	}
	if f.fail {
		return archive.Transcript{}, errors.New("PRIVATE engine output")
	}
	return archive.Transcript{Text: "Entrega confirmada", Language: "pt"}, nil
}
func TestWorkerAlwaysRemovesTemporaryAudio(t *testing.T) {
	for _, scenario := range []string{"success", "failure", "cancel", "late_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			store, err := archive.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { requireNoError(t, store.Close()) }()
			requireNoError(t, store.Track("group@g.us", true))
			_, err = store.Ingest([]archive.Message{{Chat: "group@g.us", ID: "audio", Sender: "123@lid", Group: true, Kind: "áudio", Time: time.Now(), AudioMetadata: []byte("keys")}})
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, "tmp-audio")
			requireNoError(t, os.Mkdir(root, 0700))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			engine := fakeEngine{fail: scenario == "failure"}
			if scenario == "cancel" || scenario == "late_cancel" {
				engine.cancel = cancel
				engine.ignoreCancellation = scenario == "late_cancel"
			}
			worker := Worker{Store: store, Download: fakeDownload{}, Engine: engine, Timeout: time.Minute}
			worked, err := worker.ProcessOne(ctx, root)
			if err != nil || !worked {
				t.Fatal(worked, err)
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatal("temporary audio retained", files, err)
			}
			jobs, err := store.AudioJobs(0, 50)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "success" && jobs[0].State != "done" {
				t.Fatal(jobs)
			}
			if scenario == "failure" && (jobs[0].Error == "" || jobs[0].Error == "PRIVATE engine output" || jobs[0].State != "pending" || jobs[0].Attempts != 1) {
				t.Fatal("failed transcription state or error invalid", jobs[0])
			}
			if (scenario == "cancel" || scenario == "late_cancel") && (jobs[0].Attempts != 0 || jobs[0].State == "done") {
				t.Fatal(jobs)
			}
		})
	}
}
func TestWhisperJSONPortugueseAndInvalidOutputs(t *testing.T) {
	valid := []byte(`{"model":{"multilingual":true},"params":{"translate":false},"result":{"language":"pt"},"transcription":[{"text":" Ação confirmada ✅","offsets":{"from":0,"to":2500}},{"text":" Entrega amanhã.","offsets":{"from":2500,"to":4000}}]}`)
	result, err := parseWhisper(valid)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Ação confirmada ✅\nEntrega amanhã." || len(result.Segments) != 2 || result.Segments[1].StartMS != 2500 {
		t.Fatal(result)
	}
	for _, raw := range []string{`{}`, `{"params":{"translate":true}}`, `{"result":{"language":"pt"},"transcription":[{"text":"text","offsets":{"from":5,"to":0}}]}`} {
		if _, err = parseWhisper([]byte(raw)); err == nil {
			t.Fatal("invalid output accepted")
		}
	}
}
