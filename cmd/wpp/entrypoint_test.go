package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"wpp/internal/archive"
	"wpp/internal/whatsapp"
)

func TestMainSuccessfulOfflineCommand(t *testing.T) {
	// Exercise the real entrypoint in the instrumented parent process. Failure
	// exit codes are tested separately so os.Exit cannot terminate the suite.
	previousArgs, previousOut := os.Args, os.Stdout
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Args, os.Stdout = previousArgs, previousOut; requireNoError(t, output.Close()) })
	os.Args = []string{"wpp", "--data-dir", t.TempDir(), "audio", "status"}
	os.Stdout = output
	main()
	if _, err = output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(output)
	if err != nil || !strings.Contains(string(raw), "100 registros") {
		t.Fatal(string(raw), err)
	}
}

func TestStatusIncludesMessageBoundsAndRecoveryAttempts(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	if err = store.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if err = store.Discover("group@g.us", "Demandas\n\x1b"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "group@g.us", ID: "message", Sender: "person@lid", Group: true, Kind: "texto", Text: "Ação", Time: time.Unix(1700000000, 0)}}); err != nil {
		t.Fatal(err)
	}
	attempt, err := store.BeginAttempt("group@g.us", "pending", "Verificar lacuna")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.FinishAttempt(attempt, "Verificar lacuna"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = printStatus(store, &output, time.UTC); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Mensagens: 1", "2023-11-14T22:13:20Z", "Verificar lacuna", "Demandas (group@g.us)"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatal("missing status", expected, output.String())
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if printStatus(store, &output, time.UTC) == nil || printChats(store, &output) == nil {
		t.Fatal("closed database errors lost")
	}
}

func TestMainFailureExit(t *testing.T) {
	if os.Getenv("WPP_TEST_MAIN_FAILURE") == "1" {
		os.Args = []string{"wpp", "unknown"}
		main()
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestMainFailureExit$")
	command.Env = append(os.Environ(), "WPP_TEST_MAIN_FAILURE=1")
	raw, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(raw), "Erro: comando desconhecido") {
		t.Fatal(string(raw), err)
	}
}

func TestWaitOnlineSignalsAndTimeout(t *testing.T) {
	for _, scenario := range []string{"online", "fatal", "cancel", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := &whatsapp.Client{Online: make(chan struct{}, 1), Fatal: make(chan error, 1)}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := errors.New("test connection refused")
				switch scenario {
				case "online":
					client.Online <- struct{}{}
				case "fatal":
					client.Fatal <- failure
				case "cancel":
					cancel()
				}
				err := waitOnline(ctx, client)
				if scenario == "online" && err != nil || scenario == "fatal" && !errors.Is(err, failure) || scenario == "cancel" && !errors.Is(err, context.Canceled) || scenario == "timeout" && (err == nil || !strings.Contains(err.Error(), "tempo limite")) {
					t.Fatal(scenario, err)
				}
			})
		})
	}
}

func TestOfflineErrorsDoNotCreateData(t *testing.T) {
	for _, args := range [][]string{{}, {"--unknown"}, {"audio"}, {"batch"}, {"batch", "next", "person@lid"}, {"batch", "wrong", "group@g.us"}, {"evidence"}, {"login", "extra"}, {"--batch-max-bytes", "1", "batch", "next", "group@g.us"}} {
		dir := filepath.Join(t.TempDir(), "absent")
		var out bytes.Buffer
		if err := execute(context.Background(), append([]string{"--data-dir", dir}, args...), &out, &out); err == nil {
			t.Fatal("accepted", args)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created data on validation failure", args, err)
		}
	}
	var out bytes.Buffer
	if err := execute(context.Background(), []string{"--help"}, &out, &out); err != nil || !strings.Contains(out.String(), "Comandos:") {
		t.Fatal(out.String(), err)
	}
}

func TestOfflineAudioAndEvidenceCommands(t *testing.T) {
	dir, exports := t.TempDir(), t.TempDir()
	store, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Ingest([]archive.Message{{Chat: "group@g.us", ID: "audio", Sender: "person@lid", Group: true, Kind: "áudio"}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.AudioJobs(0, 50)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	source := jobs[0].Source
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	base := []string{"--data-dir", dir, "--export-dir", exports}
	for _, command := range [][]string{{"audio", "status"}, {"evidence", "group@g.us", source}} {
		var out bytes.Buffer
		if err = execute(context.Background(), append(append([]string{}, base...), command...), &out, &out); err != nil || out.Len() == 0 {
			t.Fatal(command, out.String(), err)
		}
	}
	var out bytes.Buffer
	if err = execute(context.Background(), append(base, "audio", "retry", "absent"), &out, &out); err == nil {
		t.Fatal("missing retry accepted")
	}
}
