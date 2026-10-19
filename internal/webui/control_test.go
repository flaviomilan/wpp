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

func TestControlAPIAndReadonlyConflict(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, store)
	if err := store.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	options := service.Options{DataDir: dir, ExportDir: filepath.Join(dir, "exports"),
		Factory: func(context.Context, string, *archive.Store) (service.Transport, error) {
			return nil, errors.New("unavailable")
		},
		EngineFactory: func(context.Context, transcription.Config) (transcription.Engine, error) {
			return nil, errors.New("unavailable")
		},
	}
	controller, err := service.New(context.Background(), store, options)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, controller)
	handler := New(store, "8080", controller).Handler()
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest("GET", "http://127.0.0.1:8080/", nil))
	cookie := home.Result().Cookies()[0]
	call := func(h http.Handler, method, path, body, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		result := httptest.NewRecorder()
		h.ServeHTTP(result, req)
		return result
	}
	origin := "http://127.0.0.1:8080"
	for _, test := range []struct{ method, path, body string }{
		{"PUT", "/api/service/capture", `{"enabled":true}`},
		{"PUT", "/api/service/transcription", `{"enabled":false}`},
		{"PUT", "/api/chats/120@g.us/tracking", `{"tracked":true}`},
		{"POST", "/api/exports", `{}`},
		{"POST", "/api/session/pair", `{}`},
	} {
		if r := call(handler, test.method, test.path, test.body, "http://foreign.example"); r.Code != 403 {
			t.Fatalf("origin accepted: %s %d", test.path, r.Code)
		}
	}
	for _, body := range []string{`{}`, `{"enabled":true,"extra":1}`, `{"enabled":true} {}`} {
		if r := call(handler, "PUT", "/api/service/capture", body, origin); r.Code != 400 {
			t.Fatalf("invalid body %s: %d", body, r.Code)
		}
	}
	for _, test := range []struct{ path, body string }{
		{"/api/service/transcription", `{"enabled":false}`},
		{"/api/chats/120@g.us/tracking", `{"tracked":true}`},
	} {
		if r := call(handler, "PUT", test.path, test.body, origin); r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "120@g.us", ID: "test", Sender: "123@lid", Group: true, Kind: "texto", Text: "preserved", Time: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if r := call(handler, "PUT", "/api/chats/120@g.us/tracking", `{"tracked":false}`, origin); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if _, messages, _, err := store.Snapshot("120@g.us"); err != nil || len(messages) != 1 {
		t.Fatal("untrack removed archive", err)
	}
	response := call(handler, "POST", "/api/exports", `{"chat_id":"120@g.us"}`, origin)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	var op archive.Operation
	if err = json.Unmarshal(response.Body.Bytes(), &op); err != nil || op.ID == "" {
		t.Fatal("operation missing", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := controller.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, current := range snapshot.Operations {
			if current.ID == op.ID && current.State == "succeeded" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("export did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(handler, "GET", "/api/session/qr", "", ""); r.Code != 404 {
		t.Fatal("QR exposed without pairing")
	}
	if r := call(handler, "GET", "/api/tables/service_settings", "", ""); r.Code != 400 {
		t.Fatal("private settings exposed")
	}
	readonly, err := service.New(context.Background(), store, options)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, readonly)
	second := New(store, "8080", readonly)
	// Reuse the authenticated cookie for this second handler only after minting its own.
	h2 := second.Handler()
	home2 := httptest.NewRecorder()
	h2.ServeHTTP(home2, httptest.NewRequest("GET", origin+"/", nil))
	cookie = home2.Result().Cookies()[0]
	if r := call(h2, "PUT", "/api/service/capture", `{"enabled":true}`, origin); r.Code != 409 {
		t.Fatal("external controller allowed", r.Code)
	}
	if r := call(h2, "POST", "/api/groups/120@g.us/people", `{"id":"123@lid","name":"Mariana"}`, origin); r.Code != 200 {
		t.Fatal("readonly names blocked", r.Code, r.Body.String())
	}
}
