package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
	"wpp/internal/service"
	"wpp/internal/transcription"
)

func TestAudioAndSourceHTTPViewsAndReadonlyRetry(t *testing.T) {
	dir := t.TempDir()
	s, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, s)
	if err := s.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err := s.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	m := archive.Message{Chat: "group@g.us", ID: "original", Sender: "123@lid", Group: true, Kind: "áudio", Time: time.Now().UTC(), Wire: []byte("PRIVATE WIRE"), AudioMetadata: []byte("PRIVATE MEDIA")}
	if _, err = s.Ingest([]archive.Message{m}); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("Texto 🗓️\n", 1000)
	if err = s.CompleteAudio(*job, archive.Transcript{Text: text, Model: "test"}); err != nil {
		t.Fatal(err)
	}
	m.ID = "failure"
	if _, err = s.Ingest([]archive.Message{m}); err != nil {
		t.Fatal(err)
	}
	failed, err := s.ClaimAudio(time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseAudio(*failed, "transcrição falhou", false); err != nil {
		t.Fatal(err)
	}
	opts := service.Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"), Factory: func(context.Context, string, *archive.Store) (service.Transport, error) {
		return nil, errors.New("offline")
	}, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) {
		return nil, errors.New("unavailable")
	}}
	owner, err := service.New(context.Background(), s, opts)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, owner)
	if err := owner.SetTranscription(false); err != nil {
		t.Fatal(err)
	}
	cookie := (*http.Cookie)(nil)
	var h http.Handler
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if method != "GET" {
			req.Header.Set("Origin", "http://127.0.0.1:8080")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	h = New(s, "8080", owner).Handler()
	cookie = call("GET", "/").Result().Cookies()[0]
	for _, path := range []string{"/api/audio", "/api/audio/" + job.Source, "/api/messages/" + job.Source} {
		response := call("GET", path)
		if response.Code != 200 || strings.Contains(response.Body.String(), "PRIVATE") || strings.Contains(response.Body.String(), `"metadata"`) || strings.Contains(response.Body.String(), `"token"`) {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
	var full archive.BrowserAudio
	if err = json.Unmarshal(call("GET", "/api/audio/"+job.Source).Body.Bytes(), &full); err != nil || full.Message.Transcript.Text != text {
		t.Fatal("full transcript", err)
	}
	response := call("POST", "/api/audio/"+failed.Source+"/retry")
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	var op archive.Operation
	if err := json.Unmarshal(response.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.ID == "" || op.Source != failed.Source {
		t.Fatal(op)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, _ := owner.Snapshot()
		finished := false
		for _, current := range state.Operations {
			if current.ID == op.ID && current.State == "warning" {
				finished = true
			}
		}
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry did not enqueue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Restore an error, then prove a conflicting panel only enqueues offline.
	next, err := s.ClaimAudio(time.Now(), time.Minute)
	if err != nil || next == nil {
		t.Fatal(err)
	}
	if err := s.ReleaseAudio(*next, "transcrição falhou", false); err != nil {
		t.Fatal(err)
	}
	readonly, err := service.New(context.Background(), s, opts)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, readonly)
	h = New(s, "8080", readonly).Handler()
	cookie = nil
	cookie = call("GET", "/").Result().Cookies()[0]
	response = call("POST", "/api/audio/"+failed.Source+"/retry")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "external controller") {
		t.Fatal(response.Code, response.Body.String())
	}
	if call("GET", "/api/messages/unknown").Code != 404 {
		t.Fatal("missing source")
	}
}
