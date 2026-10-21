package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wpp/internal/archive"
)

func TestPresentationPreferencesWithoutCaptureOwner(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	closeTest(t, store)
	server := New(store, "8080")
	server.languageOverride = "pt-BR"
	handler := server.Handler()
	call := func(method, path, body, origin, host string, cookie bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		request.Host = host
		request.Header.Set("Origin", origin)
		if cookie {
			request.AddCookie(&http.Cookie{Name: "wpp_ui_8080", Value: server.token})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		return rec
	}
	read := call("GET", "/api/preferences", "", "", "127.0.0.1:8080", true)
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"application_language":"en"`) || !strings.Contains(read.Body.String(), `"initial_language":"pt-BR"`) {
		t.Fatal(read.Code, read.Body.String())
	}
	for _, body := range []string{`{"application_language":"fr"}`, `{}`, `{"application_language":"en","other":1}`, `{"application_language":"en"} {}`} {
		if rec := call("PUT", "/api/preferences", body, "http://127.0.0.1:8080", "127.0.0.1:8080", true); rec.Code != 400 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	for _, scenario := range []struct {
		host, origin string
		cookie       bool
		status       int
	}{{"evil:8080", "http://evil:8080", true, 403}, {"127.0.0.1:8080", "http://evil:8080", true, 403}, {"127.0.0.1:8080", "http://127.0.0.1:8080", false, 401}} {
		if rec := call("PUT", "/api/preferences", `{"application_language":"pt-BR"}`, scenario.origin, scenario.host, scenario.cookie); rec.Code != scenario.status {
			t.Fatal(rec.Code)
		}
	}
	if rec := call("PUT", "/api/preferences", `{"application_language":"pt-BR"}`, "http://127.0.0.1:8080", "127.0.0.1:8080", true); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	afterSave := call("GET", "/api/preferences", "", "", "127.0.0.1:8080", true)
	if !strings.Contains(afterSave.Body.String(), `"initial_language":"pt-BR"`) {
		t.Fatal("saved choice lost to initial override", afterSave.Body.String())
	}
	saved, err := archive.ReadApplicationLanguage(dir)
	if err != nil || saved != "pt-BR" {
		t.Fatal(saved, err)
	}
	if rec := call("PUT", "/api/service/capture", `{"enabled":true}`, "http://127.0.0.1:8080", "127.0.0.1:8080", true); rec.Code != 409 {
		t.Fatal("capture capability changed", rec.Code)
	}
	translations := call("GET", "/api/translations", "", "", "127.0.0.1:8080", true)
	var catalogs map[string]map[string]string
	if err = json.Unmarshal(translations.Body.Bytes(), &catalogs); err != nil || catalogs["en"]["language.saved"] == "" {
		t.Fatal(err)
	}
	broken, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = broken.Close(); err != nil {
		t.Fatal(err)
	}
	server.Store = broken
	for _, method := range []string{"GET", "PUT"} {
		if rec := call(method, "/api/preferences", `{"application_language":"en"}`, "http://127.0.0.1:8080", "127.0.0.1:8080", true); rec.Code != 500 {
			t.Fatal("write/read failure swallowed", rec.Code)
		}
	}
}
