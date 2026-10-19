package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
	"wpp/internal/service"
	"wpp/internal/transcription"
)

type panelTransport struct{ started chan struct{} }

func (p *panelTransport) Session() bool                 { return false }
func (p *panelTransport) Connected() bool               { return false }
func (p *panelTransport) Events() <-chan service.Event  { return nil }
func (p *panelTransport) Connect(context.Context) error { return nil }
func (p *panelTransport) Pair(ctx context.Context, code func(string)) error {
	code("local-test-qr")
	close(p.started)
	<-ctx.Done()
	return ctx.Err()
}
func (p *panelTransport) Discover(context.Context) error { return nil }
func (p *panelTransport) History(context.Context, archive.Message) ([]archive.Message, error) {
	return nil, nil
}
func (p *panelTransport) AudioReady() bool { return false }
func (p *panelTransport) DownloadAudio(context.Context, []byte, string) error {
	return archive.ErrAudioOffline
}
func (p *panelTransport) Close() error { return nil }

func panelCall(t *testing.T, s *Server) func(string, string, string) *httptest.ResponseRecorder {
	t.Helper()
	h := s.Handler()
	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "http://127.0.0.1:8080/", nil))
	cookie := home.Result().Cookies()[0]
	return func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		result := httptest.NewRecorder()
		h.ServeHTTP(result, req)
		return result
	}
}

func TestControlRoutesPairCancelSettingsAndValidation(t *testing.T) {
	dir := t.TempDir()
	s, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, s)
	transport := &panelTransport{started: make(chan struct{})}
	controller, err := service.New(context.Background(), s, service.Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"), Factory: func(context.Context, string, *archive.Store) (service.Transport, error) { return transport, nil }, EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) {
		return nil, errors.New("offline engine")
	}})
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, controller)
	call := panelCall(t, New(s, "8080", controller))
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/api/service", "", 200}, {"GET", "/api/service/settings", "", 200},
		{"POST", "/api/chats/discover", "", 202}, {"POST", "/api/chats/invalid/sync", "", 400}, {"POST", "/api/chats/1@g.us/sync", "", 409},
		{"PUT", "/api/chats/1@g.us/tracking", `{}`, 400}, {"PUT", "/api/chats/invalid/tracking", `{"tracked":true}`, 400},
		{"PUT", "/api/service/transcription", `{}`, 400}, {"PUT", "/api/service/capture", `{"enabled":false}`, 200},
		{"POST", "/api/exports", `{} {}`, 400}, {"POST", "/api/exports", `{"chat_id":"invalid"}`, 400},
		{"PUT", "/api/service/settings", `{"extra":1}`, 400}, {"PUT", "/api/service/settings", `{"language":"invalid"}`, 202},
		{"POST", "/api/operations/missing/cancel", "", 409},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			r := call(tc.method, tc.path, tc.body)
			if r.Code != tc.code {
				t.Fatalf("%d: %s", r.Code, r.Body.String())
			}
		})
	}
	r := call("POST", "/api/session/pair", "")
	if r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	var op archive.Operation
	if err = json.Unmarshal(r.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.started:
	case <-time.After(time.Second):
		t.Fatal("QR operation did not start")
	}
	qr := call("GET", "/api/session/qr", "")
	if qr.Code != 200 || !strings.HasPrefix(qr.Header().Get("Content-Type"), "image/png") {
		t.Fatal(qr.Code)
	}
	if r = call("POST", "/api/operations/"+op.ID+"/cancel", ""); r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
	snapshot, err := controller.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if r = call("PUT", "/api/service/settings", string(raw)); r.Code != 202 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestReadOnlyGateAndStorageErrors(t *testing.T) {
	s, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, s)
	call := panelCall(t, New(s, "8080"))
	if r := call("GET", "/api/service", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"owner":false`) {
		t.Fatal(r.Body.String())
	}
	for _, methodPath := range [][2]string{{"POST", "/api/session/pair"}, {"POST", "/api/chats/discover"}, {"POST", "/api/exports"}, {"PUT", "/api/service/capture"}, {"PUT", "/api/service/transcription"}, {"PUT", "/api/chats/1@g.us/tracking"}, {"GET", "/api/session/qr"}, {"GET", "/api/service/settings"}, {"PUT", "/api/service/settings"}} {
		if r := call(methodPath[0], methodPath[1], "{}"); r.Code != 409 {
			t.Fatal(methodPath, r.Code)
		}
	}
	if r := call("GET", "/api/tables", ""); r.Code != 200 || !strings.Contains(r.Body.String(), "messages") {
		t.Fatal(r.Body.String())
	}
	for _, path := range []string{"/api/chats/1@g.us/messages?offset=bad", "/api/chats/1@g.us/messages?limit=101", "/api/audio?offset=-1", "/api/audio?limit=bad", "/api/tables/chats?offset=bad"} {
		if r := call("GET", path, ""); r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	for _, body := range []string{`{"extra":1}`, `{"id":"1@lid","name":"Ana"} {}`} {
		if r := call("POST", "/api/groups/1@g.us/people", body); r.Code != 400 {
			t.Fatal(body, r.Code)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][2]string{{"GET", "/api/chats"}, {"GET", "/api/audio"}, {"GET", "/api/audio/missing"}, {"GET", "/api/messages/missing"}, {"GET", "/api/chats/1@g.us/messages"}, {"GET", "/api/groups/1@g.us/people"}, {"GET", "/api/tables"}, {"GET", "/api/tables/chats"}, {"POST", "/api/audio/missing/retry"}} {
		r := call(tc[0], tc[1], "")
		if r.Code != 500 || strings.Contains(r.Body.String(), "database") {
			t.Fatal(tc, r.Code, r.Body.String())
		}
	}
}
