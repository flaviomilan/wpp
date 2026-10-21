package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wpp/internal/archive"
	"wpp/internal/localization"
	"wpp/internal/webui"
)

func TestLanguageSelection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	args := []string{"--data-dir", dir}
	language, err := invocationLanguage(args)
	if err != nil || language != "en" {
		t.Fatal(language, err)
	}
	var out, diagnostic bytes.Buffer
	if err = execute(context.Background(), append(args, "--help"), &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("help created data directory", err)
	}
	if err = execute(context.Background(), append(args, "language", "pt-BR"), &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	language, err = invocationLanguage(args)
	if err != nil || language != "pt-BR" {
		t.Fatal(language, err)
	}
	language, err = invocationLanguage(append(args, "--language=en", "status"))
	if err != nil || language != "en" {
		t.Fatal(language, err)
	}
	language, err = invocationLanguage(args)
	if err != nil || language != "pt-BR" {
		t.Fatal("override persisted", language, err)
	}
	if err = execute(context.Background(), append(args, "language", "fr"), &out, &diagnostic); err == nil {
		t.Fatal("accepted fr")
	}
	if _, err = invocationLanguage([]string{"--language=fr"}); err == nil {
		t.Fatal("accepted explicit unsupported override")
	}
	db, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	saved, err := db.ApplicationLanguage()
	if err != nil || saved != "pt-BR" {
		t.Fatal(saved, err)
	}
	language, err = invocationLanguage([]string{"--data-dir", filepath.Join(t.TempDir(), "other")})
	if err != nil || language != "en" {
		t.Fatal(language, err)
	}
	if _, err = invocationLanguage([]string{"--data-dir", "/dev/null"}); err == nil {
		t.Fatal("read failure lost")
	}
}

func TestLocalizedCLIHelpAndErrors(t *testing.T) {
	for _, scenario := range []struct{ language, help, unknown string }{{"en", "Commands:", "unknown command"}, {"pt-BR", "Comandos:", "comando desconhecido"}} {
		var output bytes.Buffer
		if err := execute(context.Background(), []string{"--language", scenario.language, "--help"}, &output, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), scenario.help) {
			t.Fatal(output.String())
		}
		err := execute(context.Background(), []string{"--language", scenario.language, "unknown"}, &output, &output)
		if err == nil || !strings.Contains(err.Error(), scenario.unknown) {
			t.Fatal(err)
		}
	}
}

func TestLocalizedFailureIsNotCancellation(t *testing.T) {
	if onlyCancellation(localization.New("language.invalid")) {
		t.Fatal("a typed application failure was treated as cancellation")
	}
}

func TestDiagnosticLanguagePreservesExternalEvidence(t *testing.T) {
	if got := displayDiagnostic("pt-BR", "language.saved"); got != "Idioma da aplicação salvo." {
		t.Fatal(got)
	}
	if got := displayDiagnostic("en", "original evidence"); got != "original evidence" {
		t.Fatal(got)
	}
}

func TestWebAndCLIShareLanguageAcrossStoreReopens(t *testing.T) {
	dir := t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}()
	server := webui.New(store, "8080")
	handler := server.Handler()
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest("GET", "http://127.0.0.1:8080/", nil))
	cookie := home.Result().Cookies()[0]
	request := httptest.NewRequest("PUT", "http://127.0.0.1:8080/api/preferences", strings.NewReader(`{"application_language":"pt-BR"}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var output, diagnostic bytes.Buffer
	args := []string{"--data-dir", dir}
	if err := execute(context.Background(), append(args, "--help"), &output, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diagnostic.String(), "Comandos:") {
		t.Fatal(diagnostic.String())
	}
	if err := execute(context.Background(), append(args, "language", "en"), &output, &diagnostic); err != nil {
		t.Fatal(err)
	}
	read := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/preferences", nil)
	read.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, read)
	if !strings.Contains(response.Body.String(), `"application_language":"en"`) {
		t.Fatal(response.Body.String())
	}
}

func TestRecoveryDiagnosticsRespectLanguageAndLegacyEvidence(t *testing.T) {
	if got := attemptDiagnostic("pt-BR", archive.Attempt{DetailCode: "history.interrupted"}); got != "Execução interrompida; recuperação não confirmada." {
		t.Fatal(got)
	}
	if got := attemptDiagnostic("en", archive.Attempt{Detail: "Verificar lacuna"}); got != "Verificar lacuna" {
		t.Fatal("legacy evidence changed", got)
	}
}
