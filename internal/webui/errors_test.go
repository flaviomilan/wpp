package webui

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"wpp/internal/archive"
	"wpp/internal/service"
)

func TestPublicErrorClassification(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{sql.ErrNoRows, 404},
		{service.ErrReadOnly, 409},
		{&service.ApplicationError{Message: "Configuração inválida"}, 400},
		{&service.ApplicationError{Message: "Já em processamento", Conflict: true}, 409},
		{errors.New("PRIVATE credentials and engine output"), 500},
		{errors.Join(sql.ErrNoRows, &archive.PersistenceError{Err: errors.New("PRIVATE database path")}), 500},
	} {
		rec := httptest.NewRecorder()
		respondError(rec, test.err)
		if rec.Code != test.status || strings.Contains(rec.Body.String(), "PRIVATE") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestEmbeddedModuleAssets(t *testing.T) {
	handler := New(nil, "8080").Handler()
	for _, path := range []string{"/messages.mjs", "/i18n.mjs", "/preferences.mjs", "/app.js", "/api.mjs", "/dom.mjs", "/requests.mjs", "/chats.mjs", "/audio.mjs", "/people.mjs", "/settings.mjs", "/activity.mjs", "/inspector.mjs", "/navigation.mjs"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
