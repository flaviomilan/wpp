package main

import (
	"bytes"
	"context"
	"errors"
	"go.mau.fi/whatsmeow/types"
	"io"
	"testing"
	"wpp/internal/archive"
	"wpp/internal/whatsapp"
)

type failedOutput struct{ short bool }

func (f failedOutput) Write(raw []byte) (int, error) {
	if f.short {
		return len(raw) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func TestOutputFailuresAreReturnedInsteadOfDiscarded(t *testing.T) {
	for _, command := range [][]string{{"audio", "status"}, {"--help"}, {}} {
		args := append([]string{"--data-dir", t.TempDir()}, command...)
		err := execute(context.Background(), args, failedOutput{}, failedOutput{})
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal("writer failure lost", command, err)
		}
	}
	var stderr bytes.Buffer
	err := execute(context.Background(), []string{"--data-dir", t.TempDir(), "audio", "status"}, failedOutput{short: true}, &stderr)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write lost", err)
	}
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	requireNoError(t, store.Track("group@g.us", true))
	if !errors.Is(printStatus(store, failedOutput{}, nil), io.ErrClosedPipe) || !errors.Is(printChats(store, failedOutput{}), io.ErrClosedPipe) {
		t.Fatal("list writer failure lost")
	}
}

func TestSessionCloseAndDiagnosticErrorsJoinCommandFailure(t *testing.T) {
	failure := errors.New("test close failure")
	err := executeWithOptions(context.Background(), []string{"--data-dir", t.TempDir(), "chats"}, &bytes.Buffer{}, failedOutput{}, executeOptions{open: func(_ context.Context, _ string, _ *archive.Store, diagnostic func(string)) (*cliConnection, error) {
		diagnostic("test diagnostic")
		return &cliConnection{events: &whatsapp.Client{}, account: func() *types.JID { return nil }, close: func() error { return failure }}, nil
	}})
	if !errors.Is(err, failure) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("close or diagnostic failure lost", err)
	}
}
