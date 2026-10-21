package transcription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
	"wpp/internal/archive"
	"wpp/internal/localization"
)

type Config struct {
	Whisper, FFmpeg, Model, Language, Prompt string
	Threads                                  int
	Timeout                                  time.Duration
}
type Local struct {
	Config             Config
	ModelHash, Version string
}

func NewLocal(ctx context.Context, c Config) (local *Local, resultErr error) {
	if c.Whisper == "" {
		c.Whisper = "whisper-cli"
	}
	if c.FFmpeg == "" {
		c.FFmpeg = "ffmpeg"
	}
	if c.Language == "" {
		c.Language = "pt"
	}
	if !ValidLanguage(c.Language) {
		return nil, localization.New("backend.unsupported_audio_language_use_pt_en_es_or_auto")
	}
	if c.Threads == 0 {
		c.Threads = min(runtime.NumCPU(), 8)
	}
	if c.Threads < 1 || c.Threads > 256 {
		return nil, localization.New("backend.threads_must_be_between_1_and_256")
	}
	if c.Timeout <= 0 {
		return nil, localization.New("backend.transcription_timeout_must_be_positive")
	}
	if c.Language != "en" && strings.Contains(filepath.Base(c.Model), ".en.") {
		return nil, localization.New("backend.use_a_multilingual_model_for_this_language")
	}
	if len(c.Prompt) > 2048 {
		return nil, localization.New("backend.transcription_vocabulary_exceeds_2048_bytes")
	}
	var err error
	c.Whisper, err = exec.LookPath(c.Whisper)
	if err != nil {
		return nil, localization.New("backend.whisper_cli_not_found_configure_whisper_bin")
	}
	c.FFmpeg, err = exec.LookPath(c.FFmpeg)
	if err != nil {
		return nil, localization.New("backend.ffmpeg_not_found")
	}
	c.Model, err = filepath.Abs(c.Model)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(c.Model)
	if err != nil {
		return nil, localization.New("backend.whisper_model_not_found_configure_whisper_model")
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, localization.New("backend.invalid_whisper_model")
	}
	modelHash, err := hashModel(ctx, file)
	if err != nil {
		return nil, err
	}
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(versionCtx, c.Whisper, "--version").Output()
	if err != nil {
		return nil, localization.New("backend.could_not_run_whisper_cli_version")
	}
	version := strings.TrimSpace(string(raw))
	if len(version) > 256 {
		version = version[:256]
	}
	return &Local{Config: c, ModelHash: modelHash, Version: version}, nil
}
func (l *Local) Transcribe(ctx context.Context, audio, dir string) (archive.Transcript, error) {
	return l.TranscribePhased(ctx, audio, dir, func(string) {})
}
func (l *Local) TranscribePhased(ctx context.Context, audio, dir string, phase func(string)) (result archive.Transcript, resultErr error) {
	phase("converting")
	wav := filepath.Join(dir, "audio.wav")
	// No denoising, compression or translation: preserve the original speech.
	command := exec.CommandContext(ctx, l.Config.FFmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-protocol_whitelist", "file,pipe", "-i", audio, "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "-fs", "268435456", wav)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return result, localization.New("backend.audio_conversion_failed")
	}
	info, err := os.Stat(wav)
	if err != nil || info.Size() >= 256*1024*1024-4096 {
		return result, localization.New("backend.converted_audio_exceeds_256_mib")
	}
	output := filepath.Join(dir, "result")
	phase("transcribing")
	args := []string{"-m", l.Config.Model, "-f", wav, "-l", l.Config.Language, "-t", fmt.Sprint(l.Config.Threads), "-bs", "5", "-oj", "-of", output, "-np"}
	if l.Config.Prompt != "" {
		args = append(args, "--prompt", l.Config.Prompt)
	}
	command = exec.CommandContext(ctx, l.Config.Whisper, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err = command.Run(); err != nil {
		return result, localization.New("backend.whisper_engine_failed")
	}
	file, err := os.Open(output + ".json")
	if err != nil {
		return result, localization.New("backend.whisper_engine_did_not_produce_json")
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	raw, err := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
	if err != nil || len(raw) > 16*1024*1024 {
		return result, localization.New("backend.whisper_output_exceeds_the_limit")
	}
	result, err = parseWhisper(raw)
	if err != nil {
		return result, err
	}
	result.Model = filepath.Base(l.Config.Model)
	result.ModelSHA256 = l.ModelHash
	result.Engine = l.Version
	result.Created = time.Now().UTC()
	return result, nil
}
func parseWhisper(raw []byte) (archive.Transcript, error) {
	var output struct {
		Model         struct{ Multilingual bool }
		Params        struct{ Translate bool }
		Result        struct{ Language string }
		Transcription []struct {
			Text    string
			Offsets struct{ From, To int64 }
		}
	}
	var result archive.Transcript
	if !utf8.Valid(raw) {
		return result, localization.New("backend.whisper_json_is_not_utf_8")
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return result, localization.New("backend.invalid_whisper_json")
	}
	if output.Params.Translate {
		return result, localization.New("backend.unexpected_translation")
	}
	if output.Result.Language == "" {
		return result, localization.New("backend.language_not_identified")
	}
	var text []string
	for _, segment := range output.Transcription {
		value := strings.TrimSpace(segment.Text)
		if value == "" {
			continue
		}
		if segment.Offsets.From < 0 || segment.Offsets.To < segment.Offsets.From {
			return result, localization.New("backend.invalid_timestamps")
		}
		result.Segments = append(result.Segments, archive.TranscriptSegment{StartMS: segment.Offsets.From, EndMS: segment.Offsets.To, Text: value})
		text = append(text, value)
	}
	result.Text = strings.Join(text, "\n")
	result.Language = output.Result.Language
	if result.Text == "" || len(result.Text) > 4*1024*1024 {
		return result, localization.New("backend.speech_not_recognized_or_text_exceeds_the_limit")
	}
	return result, nil
}

func ValidLanguage(language string) bool {
	if language == "auto" {
		return true
	}
	for _, code := range strings.Fields("en zh de es ru ko fr ja pt tr pl ca nl ar sv it id hi fi vi he uk el ms cs ro da hu ta no th ur hr bg lt la mi ml cy sk te fa lv bn sr az sl kn et mk br eu is hy ne mn bs kk sq sw gl mr pa si km sn yo so af oc ka be tg sd gu am yi lo uz fo ht ps tk nn mt sa lb my bo tl mg as tt haw ln ha ba jw su yue") {
		if code == language {
			return true
		}
	}
	return false
}

// Reading between chunks is cooperatively cancellable; an OS read already in
// progress may still need to finish before cancellation can be observed.
func hashModel(ctx context.Context, model io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, modelReader{ctx: ctx, reader: model}, make([]byte, 64*1024)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type modelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r modelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return n, errors.Join(err, cancelled)
	}
	return n, err
}
