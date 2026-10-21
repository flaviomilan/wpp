package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
)

func TestEventsCLIAndOfflineCorrection(t *testing.T) {
	dir := t.TempDir()
	exports := filepath.Join(dir, "exports")
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	requireNoError(t, store.BindAccount("account@s.whatsapp.net"))
	requireNoError(t, store.Track("group@g.us", true))
	m := archive.Message{Chat: "group@g.us", ID: "audio", Sender: "ana@lid", Group: true, Kind: "áudio", Time: time.Now().UTC(), AudioMetadata: []byte("private")}
	_, err = store.Ingest([]archive.Message{m})
	requireNoError(t, err)
	job, err := store.ClaimAudio(time.Now(), time.Minute)
	if err != nil || job == nil {
		t.Fatal(job, err)
	}
	requireNoError(t, store.CompleteAudio(*job, archive.Transcript{Text: "sábado", Language: "pt", Created: time.Now().UTC()}))
	requireNoError(t, store.Close())
	var out, diagnostic bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		diagnostic.Reset()
		return execute(context.Background(), append([]string{"--data-dir", dir, "--export-dir", exports}, args...), &out, &diagnostic)
	}
	requireNoError(t, run("events", "next", "group@g.us"))
	path := strings.TrimSpace(out.String())
	if !strings.HasSuffix(path, "events.jsonl") {
		t.Fatal(path)
	}
	bytesBefore, err := os.ReadFile(path)
	requireNoError(t, err)
	correction := filepath.Join(dir, "correction.json")
	requireNoError(t, os.WriteFile(correction, []byte(`{"text":"domingo","language":"pt","engine":"manual","processed_at":"2026-10-15T18:40:00Z","segments":[{"start_ms":0,"end_ms":900,"text":"domingo"}]}`), 0600))
	requireNoError(t, run("audio", "correct", job.Source, correction))
	requireNoError(t, run("events", "next", "group@g.us"))
	bytesAfter, err := os.ReadFile(path)
	requireNoError(t, err)
	if !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("correction rewrote reserved lot")
	}
	requireNoError(t, run("events", "ack", "group@g.us", "E000001"))
	requireNoError(t, run("events", "next", "group@g.us"))
	newPath := strings.TrimSpace(out.String())
	data, err := os.ReadFile(newPath)
	requireNoError(t, err)
	if !bytes.Contains(data, []byte("domingo")) || !bytes.Contains(data, []byte(`"transcription_version":2`)) {
		t.Fatal(string(data))
	}
	requireNoError(t, run("events", "status", "group@g.us"))
	var state archive.EventStatus
	requireNoError(t, json.Unmarshal(out.Bytes(), &state))
	if state.Pending == nil {
		t.Fatal(state)
	}
	requireNoError(t, run("events", "ack", "group@g.us", "E000002"))
	requireNoError(t, run("events", "next", "group@g.us"))
	if !strings.Contains(out.String(), "No new events") {
		t.Fatal(out.String())
	}
	// Repeating an identical correction does not create another event.
	requireNoError(t, run("audio", "correct", job.Source, correction))
	requireNoError(t, run("events", "next", "group@g.us"))
	if !strings.Contains(out.String(), "No new events") {
		t.Fatal("duplicate correction")
	}
	for _, bad := range []string{`{"text":""}`, `{"text":"x","unknown":true}`, `{"text":"x"} {"text":"y"}`, `{"text":"x","segments":[{"start_ms":-1,"end_ms":0}]}`} {
		requireNoError(t, os.WriteFile(correction, []byte(bad), 0600))
		if run("audio", "correct", job.Source, correction) == nil {
			t.Fatal("invalid correction accepted", bad)
		}
	}
	if run("audio", "correct", job.Source, filepath.Join(dir, "missing")) == nil {
		t.Fatal("missing file accepted")
	}
	if run("events", "ack", "group@g.us", "missing") == nil {
		t.Fatal("unknown lot accepted")
	}
	if run("events", "next", "not-a-group") == nil {
		t.Fatal("invalid group accepted")
	}
}
