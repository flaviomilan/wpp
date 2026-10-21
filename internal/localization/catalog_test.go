package localization

import (
	"errors"
	"regexp"
	"slices"
	"testing"
)

func TestCatalogs(t *testing.T) {
	for key, english := range catalogs[English] {
		portuguese, ok := catalogs[Portuguese][key]
		if !ok {
			t.Fatalf("missing Portuguese key %s", key)
		}
		pattern := regexp.MustCompile(`\{[^}]+\}`)
		left, right := pattern.FindAllString(english, -1), pattern.FindAllString(portuguese, -1)
		slices.Sort(left)
		slices.Sort(right)
		if !slices.Equal(left, right) {
			t.Fatalf("parameters differ for %s", key)
		}
	}
	if len(catalogs[English]) != len(catalogs[Portuguese]) {
		t.Fatal("catalog key counts differ")
	}
	if Text("invalid", "count.other", map[string]string{"count": "2"}) != "2 items" {
		t.Fatal("English fallback or substitution")
	}
	if Text(Portuguese, "unknown", nil) != catalogs[English]["error.generic"] {
		t.Fatal("unknown key leaked")
	}
	saved := catalogs[Portuguese]["language.saved"]
	delete(catalogs[Portuguese], "language.saved")
	defer func() { catalogs[Portuguese]["language.saved"] = saved }()
	if Text(Portuguese, "language.saved", nil) != catalogs[English]["language.saved"] {
		t.Fatal("missing translation fallback")
	}
	if !Valid(English) || !Valid(Portuguese) || Valid("pt") || Normalize("") != English {
		t.Fatal("language validation")
	}
}

func TestLocalizedErrors(t *testing.T) {
	original := New("language.saved")
	translated := InLanguage(original, Portuguese)
	if translated.Error() != Text(Portuguese, "language.saved", nil) {
		t.Fatal(translated)
	}
	if InLanguage(nil, English) != nil || Render(nil, English) != "" {
		t.Fatal("nil error")
	}
	if Render(errors.Join(original, errors.New("external")), English) != "Application language saved.\nexternal" {
		t.Fatal("joined render")
	}
	wrapped := Errorf("cli.unknown_command_s", "bad")
	if wrapped.Error() != "unknown command: bad" {
		t.Fatal(wrapped)
	}
	if Render(wrapped, Portuguese) != "comando desconhecido: bad" {
		t.Fatal("format rendering")
	}
	if !errors.Is(translated, original) {
		t.Fatal("lost error identity")
	}
	var message *MessageError
	if !errors.As(wrapped, &message) || message.Unwrap() == nil {
		t.Fatal("wrapped formatting cause")
	}
	var originalMessage *MessageError
	if !errors.As(original, &originalMessage) || originalMessage.Unwrap() != nil {
		t.Fatal("unexpected cause")
	}
}

func TestMessageIdentity(t *testing.T) {
	var message *MessageError
	if !errors.As(New("language.invalid"), &message) || message.MessageCode() != "language.invalid" {
		t.Fatal("message identity missing")
	}
}

func TestParametersAreNeverReinterpreted(t *testing.T) {
	if got := Text(English, "route.self_named", map[string]string{"name": "{name}"}); got != "{name} (you)" {
		t.Fatal(got)
	}
}

func TestUnknownErrorIdentifiersDoNotExposeParameters(t *testing.T) {
	err := Errorf("unknown.identifier", "PRIVATE")
	if err.Error() != Text(English, "error.generic", nil) || Render(err, Portuguese) != Text(English, "error.generic", nil) {
		t.Fatal("unknown message exposed parameters")
	}
}
