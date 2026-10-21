package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wpp/internal/archive"
)

func TestOfflineCLIWorkflow(t *testing.T) {
	dir := t.TempDir()
	exports := filepath.Join(t.TempDir(), "exports")
	var out, stderr bytes.Buffer
	base := []string{"--data-dir", dir, "--export-dir", exports}
	for _, cmd := range [][]string{{"track", "120@g.us"}, {"status"}, {"export"}, {"untrack", "120@g.us"}, {"status"}} {
		args := append(append([]string{}, base...), cmd...)
		if err := execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v", cmd, err)
		}
	}
	if !strings.Contains(out.String(), "unconfirmed") {
		t.Fatal("status lacks uncertainty")
	}
}

func TestBatchCLIWorksWhileCaptureLockIsHeld(t *testing.T) {
	dir := t.TempDir()
	db, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err = db.Track("120@g.us", true); err != nil {
		t.Fatal(err)
	}
	_, err = db.Ingest([]archive.Message{{Chat: "120@g.us", ID: "m", Sender: "person@s.whatsapp.net", Group: true, Time: time.Unix(10, 0), Kind: "texto", Text: "Demandas em andamento"}})
	if err != nil {
		t.Fatal(err)
	}
	requireNoError(t, db.Close())
	unlock, err := archive.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	base := []string{"--data-dir", dir, "--export-dir", t.TempDir()}
	for _, cmd := range [][]string{{"batch", "next", "120@g.us"}, {"batch", "status", "120@g.us"}, {"batch", "ack", "120@g.us", "E000001"}, {"batch", "ack", "120@g.us", "E000001"}, {"batch", "next", "120@g.us"}} {
		var out bytes.Buffer
		if err = execute(context.Background(), append(append([]string{}, base...), cmd...), &out, &out); err != nil {
			t.Fatalf("%v: %v", cmd, err)
		}
	}
}

func TestChatListingIdentifiesUnnamedGroupsAndContacts(t *testing.T) {
	db, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, db.Close()) }()
	for _, entry := range [][2]string{{"120@g.us", ""}, {"121@g.us", "Grupo conhecido"}, {"551100000000@s.whatsapp.net", ""}} {
		if err = db.Discover(entry[0], entry[1]); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err = printChats(db, &out); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"TYPE", "group", "contact", "Grupo conhecido", "Group (name unavailable)", "Contact (name unavailable)"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("listagem sem identificação esperada %q: %s", expected, out.String())
		}
	}
	out.Reset()
	if err = printChats(db, &out, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "551100000000") || !strings.Contains(out.String(), "120@g.us") {
		t.Fatalf("filtro de grupos incorreto: %s", out.String())
	}
}
func TestCLIValidationBeforeCreatingData(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"track"}, {"track", "name"}, {"track", "status@broadcast"}, {"status", "extra"}, {"chats", "--unknown"}, {"--timezone", "invalid", "status"}} {
		var out bytes.Buffer
		if execute(context.Background(), args, &out, &out) == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
}

func TestOnlyCancellationPreservesWrappedShutdownFailures(t *testing.T) {
	for _, err := range []error{nil, cancellationMatch{}, context.Canceled, fmt.Errorf("wrapped: %w", context.Canceled), errors.Join(context.Canceled, context.Canceled)} {
		if !onlyCancellation(err) {
			t.Fatalf("cancellation rejected: %v", err)
		}
	}
	failure := errors.New("export failed")
	for _, err := range []error{failure, errors.Join(cancellationMatch{}, failure), errors.Join(context.Canceled, failure), fmt.Errorf("wrapped: %w", errors.Join(context.Canceled, failure))} {
		if onlyCancellation(err) {
			t.Fatalf("failure discarded: %v", err)
		}
	}
}

type cancellationMatch struct{}

func (cancellationMatch) Error() string        { return "custom cancellation" }
func (cancellationMatch) Is(target error) bool { return target == context.Canceled }
