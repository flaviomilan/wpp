package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
)

func TestPanelReadWriteBoundary(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, store)
	if err = store.Track("test@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "test@g.us", ID: "one", Sender: "123@lid", Group: true, Author: "Recebido", Kind: "texto", Text: "Conteúdo original", Time: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	server := New(store, "8080")
	handler := server.Handler()
	call := func(method, path, body, host, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		request.Host = host
		request.Header.Set("Origin", origin)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		return rec
	}
	if r := call("GET", "/api/chats", "", "127.0.0.1:8080", "", nil); r.Code != 401 {
		t.Fatal(r.Code)
	}
	home := call("GET", "/", "", "127.0.0.1:8080", "", nil)
	if home.Code != 200 || !strings.Contains(home.Body.String(), "People directory") {
		t.Fatal(home.Code)
	}
	cookie := home.Result().Cookies()[0]
	audio := archive.Message{Chat: "test@g.us", ID: "audio", Sender: "123@lid", Group: true, Kind: "áudio", Time: time.Now().UTC(), AudioMetadata: []byte("PRIVATE KEY")}
	if _, err = store.Ingest([]archive.Message{audio}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/audio", "/api/tables/audio_jobs"} {
		r := call("GET", path, "", "127.0.0.1:8080", "", cookie)
		if r.Code != 200 || strings.Contains(r.Body.String(), "metadata") || strings.Contains(r.Body.String(), "PRIVATE KEY") {
			t.Fatal("queue metadata exposed", r.Body.String())
		}
	}
	if r := call("POST", "/api/audio/unknown/retry", "", "127.0.0.1:8080", "http://attacker.example", cookie); r.Code != 403 {
		t.Fatal("cross origin retry allowed")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie protection")
	}
	if r := call("GET", "/api/chats", "", "attacker.example:8080", "", cookie); r.Code != 403 {
		t.Fatal("foreign host accepted")
	}
	payload := "{\"id\":\"123@lid\",\"name\":\"Maria\"}"
	if r := call("POST", "/api/groups/test@g.us/people", payload, "127.0.0.1:8080", "https://attacker.example", cookie); r.Code != 403 {
		t.Fatal("cross origin write")
	}
	if r := call("POST", "/api/groups/test@g.us/people", payload, "127.0.0.1:8080", "http://127.0.0.1:8080", cookie); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	page := call("GET", "/api/chats/test@g.us/messages?q=Conte%C3%BAdo", "", "127.0.0.1:8080", "", cookie)
	var messages archive.MessagePage
	if err = json.Unmarshal(page.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages.Items) != 1 || messages.Items[0].Author != "Maria" || messages.Items[0].Message.Text != "Conteúdo original" {
		t.Fatal(messages)
	}
	for _, path := range []string{"/api/tables/session", "/api/tables/sqlite_master", "/api/tables/messages?limit=1000"} {
		if r := call("GET", path, "", "127.0.0.1:8080", "", cookie); r.Code != 400 {
			t.Fatal(path, r.Code)
		}
	}
	if r := call("POST", "/api/tables/messages", "{}", "127.0.0.1:8080", "http://127.0.0.1:8080", cookie); r.Code != 405 {
		t.Fatal("inspector write endpoint", r.Code)
	}
	if r := call("POST", "/api/groups/test@g.us/people", "{\"id\":\"123@lid\",\"name\":\"Ana\",\"Text\":\"alterado\"}", "127.0.0.1:8080", "http://127.0.0.1:8080", cookie); r.Code != 400 {
		t.Fatal("unknown field accepted")
	}
	if r := call("GET", "/api/tables/messages", "", "127.0.0.1:8080", "", cookie); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestServeRejectsPublicListener(t *testing.T) {
	if err := ServeControlled(context.Background(), nil, "0.0.0.0:8080", func(string) {}, time.UTC, nil); err == nil {
		t.Fatal("public listener accepted")
	}
}
