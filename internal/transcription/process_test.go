package transcription

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const sampleJSON = `{"result":{"language":"pt"},"transcription":[{"text":" Ação confirmada ✅","offsets":{"from":0,"to":1000}}]}`

func executable(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func localConfig(t *testing.T) Config {
	t.Helper()
	model := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(model, []byte("test model"), 0600); err != nil {
		t.Fatal(err)
	}
	return Config{Model: model, Timeout: time.Minute,
		FFmpeg:  executable(t, "ffmpeg", "for arg do last=$arg; done\nprintf wav > \"$last\"\n"),
		Whisper: executable(t, "whisper", "if [ \"$1\" = --version ]; then printf 'test-engine\\n'; exit; fi\nwhile [ \"$#\" -gt 0 ]; do if [ \"$1\" = -of ]; then shift; output=$1; fi; shift; done\nprintf '%s' '"+sampleJSON+"' > \"$output.json\"\n"),
	}
}

func TestLocalPipelineRunsExecutablesAndRecordsProvenance(t *testing.T) {
	cfg := localConfig(t)
	cfg.Prompt = "Aurora, ação"
	engine, err := NewLocal(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Config.Language != "pt" || engine.Config.Threads < 1 || engine.Config.Threads > 8 || len(engine.ModelHash) != 64 || engine.Version != "test-engine" {
		t.Fatal(engine)
	}
	dir := t.TempDir()
	var phases []string
	result, err := engine.TranscribePhased(context.Background(), filepath.Join(dir, "input"), dir, func(p string) { phases = append(phases, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phases, []string{"converting", "transcribing"}) || result.Text != "Ação confirmada ✅" || result.ModelSHA256 != engine.ModelHash || result.Engine != engine.Version || result.Model != "model.bin" || result.Created.IsZero() {
		t.Fatal(phases, result)
	}
	if result, err = engine.Transcribe(context.Background(), "input", dir); err != nil || result.Text != "Ação confirmada ✅" {
		t.Fatal(result, err)
	}
}

func TestLocalConfigurationRejectsInvalidInputs(t *testing.T) {
	for _, scenario := range []string{"language", "threads", "timeout", "monolingual", "prompt", "whisper", "ffmpeg", "model", "directory", "version", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := localConfig(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "language":
				cfg.Language = "invalid"
			case "threads":
				cfg.Threads = 257
			case "timeout":
				cfg.Timeout = 0
			case "monolingual":
				cfg.Model = "model.en.bin"
			case "prompt":
				cfg.Prompt = strings.Repeat("a", 2049)
			case "whisper":
				cfg.Whisper = filepath.Join(t.TempDir(), "missing")
			case "ffmpeg":
				cfg.FFmpeg = filepath.Join(t.TempDir(), "missing")
			case "model":
				cfg.Model = filepath.Join(t.TempDir(), "missing")
			case "directory":
				cfg.Model = t.TempDir()
			case "version":
				cfg.Whisper = executable(t, "whisper", "exit 1\n")
			case "cancel":
				cancel()
			}
			if _, err := NewLocal(ctx, cfg); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestLocalPipelineFailuresArePublicAndPhasesStop(t *testing.T) {
	for _, scenario := range []string{"conversion", "missing-wav", "large-wav", "whisper", "missing-json", "invalid-json", "large-json", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := localConfig(t)
			engine, err := NewLocal(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "conversion":
				engine.Config.FFmpeg = executable(t, "ffmpeg", "printf 'private' >&2; exit 1\n")
			case "missing-wav":
				engine.Config.FFmpeg = executable(t, "ffmpeg", "exit 0\n")
			case "large-wav":
				engine.Config.FFmpeg = executable(t, "ffmpeg", "for arg do last=$arg; done\ntruncate -s 268435456 \"$last\"\n")
			case "whisper":
				engine.Config.Whisper = executable(t, "whisper", "printf 'private' >&2; exit 1\n")
			case "missing-json":
				engine.Config.Whisper = executable(t, "whisper", "exit 0\n")
			case "invalid-json":
				engine.Config.Whisper = executable(t, "whisper", "while [ \"$#\" -gt 0 ]; do if [ \"$1\" = -of ]; then shift; output=$1; fi; shift; done\nprintf invalid > \"$output.json\"\n")
			case "large-json":
				engine.Config.Whisper = executable(t, "whisper", "while [ \"$#\" -gt 0 ]; do if [ \"$1\" = -of ]; then shift; output=$1; fi; shift; done\ntruncate -s 16777217 \"$output.json\"\n")
			case "cancel":
				cancel()
			}
			var phases []string
			_, err = engine.TranscribePhased(ctx, "input", t.TempDir(), func(p string) { phases = append(phases, p) })
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if (scenario == "conversion" || scenario == "missing-wav" || scenario == "large-wav" || scenario == "cancel") && len(phases) != 1 {
				t.Fatal(phases)
			}
		})
	}
}

func TestWhisperParserRejectsMalformedAndEmptyContent(t *testing.T) {
	for _, raw := range [][]byte{[]byte("invalid"), {0xff}, []byte(`{"result":{"language":"pt"},"transcription":[{"text":"  "}]}`), []byte(`{"result":{"language":"pt"},"transcription":[{"text":"x","offsets":{"from":-1,"to":2}}]}`)} {
		if _, err := parseWhisper(raw); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
}
