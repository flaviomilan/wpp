package transcription

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
)

type sampleDownloader struct{ file string }

func (d sampleDownloader) DownloadAudio(_ context.Context, _ []byte, path string) error {
	raw, err := os.ReadFile(d.file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

// Opt-in smoke test using an actual local model, never WhatsApp account data.
func TestRealWhisperPipeline(t *testing.T) {
	model := os.Getenv("WPP_TEST_MODEL")
	if model == "" {
		t.Skip("set WPP_TEST_MODEL, WPP_TEST_WHISPER and WPP_TEST_AUDIO for a real engine test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	engine, err := NewLocal(ctx, Config{Whisper: os.Getenv("WPP_TEST_WHISPER"), Model: model, Language: "en", Timeout: 5 * time.Minute, Threads: 4})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	if err = store.Track("sample@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "sample@g.us", ID: "jfk", Sender: "speaker@lid", Group: true, Kind: "áudio", Time: time.Now(), AudioMetadata: []byte("sample")}}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "tmp-audio")
	requireNoError(t, os.Mkdir(root, 0700))
	worker := Worker{Store: store, Download: sampleDownloader{os.Getenv("WPP_TEST_AUDIO")}, Engine: engine, Timeout: 5 * time.Minute}
	worked, err := worker.ProcessOne(ctx, root)
	if err != nil || !worked {
		t.Fatal(err)
	}
	page, err := store.BrowseMessages("sample@g.us", "", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	transcript := page.Items[0].Message.Transcript
	if transcript == nil {
		jobs, _ := store.AudioJobs(0, 50)
		t.Fatal("real transcription failed", jobs)
	}
	if !strings.Contains(strings.ToLower(transcript.Text), "country") || len(transcript.ModelSHA256) != 64 {
		t.Fatal("unexpected real sample transcript")
	}
	files, _ := os.ReadDir(root)
	if len(files) > 0 {
		t.Fatal("audio retained")
	}
}
