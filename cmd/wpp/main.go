package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
	_ "time/tzdata"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"wpp/internal/archive"
	"wpp/internal/localization"
	"wpp/internal/service"
	"wpp/internal/transcription"
	"wpp/internal/webui"
	"wpp/internal/whatsapp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !onlyCancellation(err) {
		// The exit status still reports failure if stderr itself cannot be written.
		prefix := "Error:"
		var translated *localization.LocalizedError
		if errors.As(err, &translated) && translated.Language == localization.Portuguese {
			prefix = "Erro:"
		}
		_, _ = fmt.Fprintln(os.Stderr, prefix, err)
		os.Exit(1)
	}
}

type cliConnection struct {
	events    *whatsapp.Client
	account   func() *types.JID
	qr        func(context.Context) (<-chan whatsmeow.QRChannelItem, error)
	connect   func(context.Context) error
	discover  func(context.Context) error
	close     func() error
	reconnect func()
}

type executeOptions struct {
	open    func(context.Context, string, *archive.Store, func(string)) (*cliConnection, error)
	service service.Options
}

// checkedOutput retains writer failures, including callbacks whose signatures
// cannot return errors. Command completion joins them with execution failures.
type checkedOutput struct {
	writer   io.Writer
	language string
	mu       sync.Mutex
	err      error
}

func (o *checkedOutput) Write(raw []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.writer.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	o.err = err
	return n, err
}
func (o *checkedOutput) failure() error                    { o.mu.Lock(); defer o.mu.Unlock(); return o.err }
func (o *checkedOutput) println(args ...any)               { _, _ = fmt.Fprintln(o, args...) }
func (o *checkedOutput) printf(format string, args ...any) { _, _ = fmt.Fprintf(o, format, args...) }

func openCLIConnection(ctx context.Context, dir string, store *archive.Store, fail func(string)) (*cliConnection, error) {
	client, err := whatsapp.Open(ctx, dir, store, fail)
	if err != nil {
		return nil, err
	}
	return &cliConnection{events: client, account: func() *types.JID { return client.WA.Store.ID }, qr: client.WA.GetQRChannel, connect: client.WA.ConnectContext, discover: client.Discover, close: client.Close, reconnect: func() { client.WA.InitialAutoReconnect = true }}, nil
}

func execute(ctx context.Context, args []string, out, stderr io.Writer) error {
	return executeWithOptions(ctx, args, out, stderr, executeOptions{})
}

func executeWithOptions(ctx context.Context, args []string, out, stderr io.Writer, options executeOptions) (resultErr error) {
	language, languageErr := invocationLanguage(args)
	if languageErr != nil {
		return languageErr
	}
	output, diagnostic := &checkedOutput{writer: out, language: language}, &checkedOutput{writer: stderr, language: language}
	defer func() {
		resultErr = localization.InLanguage(errors.Join(resultErr, output.failure(), diagnostic.failure()), language)
	}()
	flags := flag.NewFlagSet("wpp", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	languageOverride := flags.String("language", "", localization.Text(language, "language.override_help", nil))
	data := flags.String("data-dir", "./data", localization.Text(language, "cli.session_and_database_directory", nil))
	exports := flags.String("export-dir", "./exports", localization.Text(language, "cli.jsonl_export_directory", nil))
	timezone := flags.String("timezone", "America/Sao_Paulo", localization.Text(language, "cli.display_time_zone_for_the_panel_and_status", nil))
	listen := flags.String("listen", "127.0.0.1:8082", localization.Text(language, "cli.local_web_interface_address", nil))
	audioConfig := transcription.Config{}
	flags.StringVar(&audioConfig.Model, "whisper-model", "", localization.Text(language, "cli.multilingual_ggml_model_enables_transcription_during_run", nil))
	flags.StringVar(&audioConfig.Whisper, "whisper-bin", "whisper-cli", localization.Text(language, "cli.whisper_cpp_executable", nil))
	flags.StringVar(&audioConfig.FFmpeg, "ffmpeg-bin", "ffmpeg", localization.Text(language, "cli.ffmpeg_executable", nil))
	flags.StringVar(&audioConfig.Language, "audio-language", "pt", localization.Text(language, "cli.audio_recognition_language_auto_to_detect", nil))
	flags.StringVar(&audioConfig.Prompt, "audio-vocabulary", "", localization.Text(language, "cli.names_and_specific_terms_to_guide_recognition", nil))
	flags.IntVar(&audioConfig.Threads, "audio-threads", 0, localization.Text(language, "cli.transcription_threads_0_uses_up_to_8_cpus", nil))
	flags.DurationVar(&audioConfig.Timeout, "audio-timeout", 30*time.Minute, localization.Text(language, "cli.time_limit_per_audio_without_truncating_content", nil))
	flags.Usage = func() {
		diagnostic.println(localization.Text(language, "cli.usage_wpp_global_options_command_commands_login_chats_groups", nil))
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		flags.Usage()
		return localization.New("cli.specify_a_command")
	}
	command := rest[0]
	switch command {
	case "language":
		if len(rest) > 2 {
			return localization.New("language.usage")
		}
		if len(rest) == 2 && !localization.Valid(rest[1]) {
			return errors.New(localization.Text(language, "language.invalid", nil))
		}
	case "audio":
		if len(rest) == 4 && rest[1] == "correct" {
			break
		}
		if (len(rest) == 2 && rest[1] == "status") || (len(rest) == 3 && rest[1] == "retry") {
			break
		}
		return localization.New("cli.usage_wpp_audio_status_wpp_audio_retry_source_or")
	case "events", "batch":
		if len(rest) < 3 {
			return localization.Errorf("cli.usage_wpp_s_next_status_group_or_s_ack", command, command)
		}
		if !strings.HasSuffix(rest[2], "@g.us") {
			return localization.New("cli.use_a_group_identifier_ending_in_g_us")
		}
		if (rest[1] == "ack" && len(rest) != 4) || ((rest[1] == "next" || rest[1] == "status") && len(rest) != 3) || (rest[1] != "ack" && rest[1] != "next" && rest[1] != "status") {
			return localization.Errorf("cli.invalid_subcommand_or_arguments_for_s", command)
		}

	case "evidence":
		if len(rest) != 3 {
			return localization.New("cli.usage_wpp_evidence_group_source")
		}

	case "track", "untrack":
		if len(rest) != 2 {
			return errors.New(localization.Text(language, "cli.usage_wpp", nil) + command + " <id>")
		}
		j, err := types.ParseJID(rest[1])
		if err != nil || !whatsapp.Supported(j) || j.User == "" || j.Device != 0 {
			return localization.New("cli.use_the_full_group_or_contact_id_shown_by")
		}
	case "chats":
		if len(rest) > 2 || (len(rest) == 2 && rest[1] != "--groups") {
			return localization.New("cli.usage_wpp_chats_groups")
		}
	case "login", "run", "status", "export", "serve":
		if len(rest) != 1 {
			return localization.New("cli.unexpected_arguments")
		}
	default:
		return localization.Errorf("cli.unknown_command_s", command)
	}
	zone, err := time.LoadLocation(*timezone)
	if err != nil {
		return localization.New("cli.invalid_time_zone")
	}
	// Canonicalize the data path before acquiring the lock (including symlinks).
	if err = os.MkdirAll(*data, 0700); err != nil {
		return err
	}
	*data, err = filepath.EvalSymlinks(*data)
	if err != nil {
		return err
	}
	*data, err = filepath.Abs(*data)
	if err != nil {
		return err
	}
	if command == "events" || command == "batch" || command == "evidence" {
		group := rest[2]
		if command == "evidence" {
			group = rest[1]
		}
		unlock, e := archive.AnalysisLock(*data, eventLockGroup(command, group))
		if e != nil {
			return e
		}
		defer unlock()
	} else if command != "status" && command != "serve" && command != "run" && command != "audio" && command != "language" {
		unlock, e := archive.Lock(*data)
		if e != nil {
			return e
		}
		defer unlock()
	}
	db, err := archive.Open(*data)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if command == "language" {
		if len(rest) == 2 {
			if err = db.SaveApplicationLanguage(rest[1]); err != nil {
				return err
			}
		}
		saved, e := db.ApplicationLanguage()
		if e != nil {
			return e
		}
		output.println(saved)
		return nil
	}

	if command == "audio" {
		if rest[1] == "correct" {
			file, e := os.Open(rest[3])
			if e != nil {
				return e
			}
			var input struct {
				Text        string    `json:"text"`
				Language    string    `json:"language"`
				Model       string    `json:"model"`
				ModelSHA256 string    `json:"model_sha256"`
				Engine      string    `json:"engine"`
				ProcessedAt time.Time `json:"processed_at"`
				Segments    []struct {
					StartMS int64  `json:"start_ms"`
					EndMS   int64  `json:"end_ms"`
					Text    string `json:"text"`
				} `json:"segments"`
			}
			info, statErr := file.Stat()
			if statErr != nil {
				return errors.Join(statErr, file.Close())
			}
			if info.Size() > 16*1024*1024 {
				return errors.Join(localization.New("cli.transcript_json_exceeds_16_mib"), file.Close())
			}
			decoder := json.NewDecoder(io.LimitReader(file, 16*1024*1024+1))
			decoder.DisallowUnknownFields()
			e = decoder.Decode(&input)
			var extra any
			if e == nil {
				if next := decoder.Decode(&extra); !errors.Is(next, io.EOF) {
					e = localization.New("cli.provide_a_single_transcript_json_object")
				}
			}
			e = errors.Join(e, file.Close())
			if e != nil {
				return e
			}
			t := archive.Transcript{Text: input.Text, Language: input.Language, Model: input.Model, ModelSHA256: input.ModelSHA256, Engine: input.Engine, Created: input.ProcessedAt}
			for _, segment := range input.Segments {
				t.Segments = append(t.Segments, archive.TranscriptSegment{StartMS: segment.StartMS, EndMS: segment.EndMS, Text: segment.Text})
			}
			return db.CorrectTranscript(rest[2], t)
		}
		if rest[1] == "retry" {
			return db.RetryAudio(rest[2])
		}
		jobs, e := db.AudioJobs(0, 100)
		if e != nil {
			return e
		}
		summary, e := db.AudioSummary()
		if e != nil {
			return e
		}
		summaryLabels := map[string]int{}
		for state, count := range summary {
			label := state
			if localization.Known("state." + state) {
				label = localization.Text(language, "state."+state, nil)
			}
			summaryLabels[label] = count
		}
		output.println(localization.Text(language, "cli.transcription_queue", nil), summaryLabels)
		for _, job := range jobs {
			output.printf(localization.Text(language, "cli.s_s_s_attempts_d_s", nil), job.Source, job.Chat, localization.Text(language, "state."+job.State, nil), job.Attempts, displayDiagnostic(language, job.Error))
		}
		output.println(localization.Text(language, "cli.up_to_100_recent_records_browse_all_pages_in", nil))
		return nil
	}
	if command == "serve" || command == "run" {
		opts := options.service
		opts.DataDir, opts.ExportDir, opts.Zone = *data, *exports, zone
		if audioConfig.Model != "" {
			opts.Audio = &audioConfig
		}
		controller, e := service.New(ctx, db, opts)
		if e != nil {
			return e
		}
		defer func() { resultErr = errors.Join(resultErr, controller.Close()) }()
		if command == "serve" {
			return webui.ServeControlled(ctx, db, *listen, func(url string) {
				output.println(localization.Text(language, "cli.control_panel", nil), url)
				output.println(localization.Text(language, "cli.closing_the_tab_keeps_processing_active_ctrl_c_stops", nil))
			}, zone, controller, *languageOverride)
		}
		state, e := controller.Snapshot()
		if e != nil {
			return e
		}
		if !state.Owner {
			return service.ErrReadOnly
		}
		if !state.Linked {
			return localization.New("cli.run_wpp_login_first")
		}
		chats, e := db.Chats()
		if e != nil {
			return e
		}
		tracked := false
		for _, chat := range chats {
			tracked = tracked || chat.Tracked
		}
		if !tracked {
			return localization.New("cli.configure_at_least_one_chat_with_wpp_track_id")
		}
		if e = controller.SetCapture(true); e != nil {
			return e
		}
		output.println(localization.Text(language, "cli.capture_requested_ctrl_c_stops_capture_and_completes_pending", nil))
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				state, e = controller.Snapshot()
				if e != nil {
					return e
				}
				if state.Connection == "needs_login" || state.Connection == "error" {
					return errors.New(state.Error)
				}
			}
		}
	}

	if command == "events" || command == "batch" {
		batches := archive.EventBatchService{Store: db, Dir: *exports}
		switch rest[1] {
		case "next":
			path, e := batches.Next(rest[2])
			if e != nil {
				return e
			}
			if path == "" {
				output.println(localization.Text(language, "cli.no_new_events", nil))
			} else {
				output.println(path)
			}
			return nil
		case "ack":
			if e := batches.Ack(rest[2], rest[3]); e != nil {
				return e
			}
			output.println(localization.Text(language, "cli.batch_acknowledged", nil), rest[3])
			return nil
		case "status":
			state, e := db.EventStatus(rest[2])
			if e != nil {
				return e
			}
			return json.NewEncoder(output).Encode(state)
		}
	}
	exporter := archive.Exporter{Store: db, Dir: *exports}
	if command == "evidence" {
		batches := archive.EventBatchService{Store: db, Dir: *exports}
		path, e := batches.Evidence(rest[1], rest[2])
		if e != nil {
			return e
		}
		output.println(path)
		return nil
	}

	switch command {
	case "track", "untrack":
		if err = db.Track(rest[1], command == "track"); err != nil {
			return err
		}
		output.println(localization.Text(language, "cli.tracking_updated", nil), rest[1])
		return exporter.ExportContext(ctx, false)
	case "status":
		return printStatus(db, output, zone)
	case "export":
		if err = exporter.ExportContext(ctx, true); err != nil {
			return err
		}
		output.println(localization.Text(language, "cli.jsonl_files_rebuilt_in", nil), *exports)
		return nil
	}

	open := options.open
	if open == nil {
		open = openCLIConnection
	}
	client, err := open(ctx, *data, db, func(message string) { diagnostic.println(displayDiagnostic(language, message)) })
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, client.close()) }()
	if command == "login" {
		if client.account() != nil {
			if err = db.BindAccount(client.account().ToNonAD().String()); err != nil {
				return err
			}
			output.println(localization.Text(language, "cli.session_already_linked_run_wpp_chats_or_wpp_run", nil))
			return nil
		}
		qr, err := client.qr(ctx)
		if err != nil {
			return err
		}
		if err = client.connect(ctx); err != nil {
			return localization.New("cli.could_not_connect_to_whatsapp")
		}
		output.println(localization.Text(language, "cli.on_your_phone_whatsapp_linked_devices_link_a_device", nil))
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-client.events.Fatal:
				return err
			case item, ok := <-qr:
				if !ok {
					return localization.New("cli.pairing_ended_try_wpp_login_again")
				}
				if item.Event == "code" {
					qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, output)
				}
				if item.Event == "success" {
					if err = waitOnline(ctx, client.events); err != nil {
						return err
					}
					output.println(localization.Text(language, "cli.account_linked_waiting_10_seconds_for_initial_synchronization", nil))
					timer := time.NewTimer(10 * time.Second)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						return ctx.Err()
					case err := <-client.events.Fatal:
						return err
					case <-timer.C:
					}
					if err = client.discover(ctx); err != nil {
						diagnostic.println(localization.Text(language, "cli.discovery_incomplete_run_wpp_chats_again", nil))
					}
					return nil
				}
				if item.Event == "timeout" || item.Event == "error" {
					return localization.New("cli.pairing_incomplete_try_again")
				}
			}
		}
	}
	if client.account() == nil {
		return localization.New("cli.run_wpp_login_first")
	}
	if err = db.BindAccount(client.account().ToNonAD().String()); err != nil {
		return err
	}

	client.reconnect()
	if err = client.connect(ctx); err != nil {
		return localization.New("cli.could_not_connect_to_whatsapp")
	}
	if command == "chats" {
		if err = waitOnline(ctx, client.events); err != nil {
			return err
		}
		// Allow recent chat metadata to arrive even when contact/group queries finish quickly.
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-client.events.Fatal:
			return err
		case <-timer.C:
		}
		if err = client.discover(ctx); err != nil {
			return err
		}
		return printChats(db, output, len(rest) == 2)
	}
	return localization.New("cli.invalid_command")
}

func waitOnline(ctx context.Context, c *whatsapp.Client) error {
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.Fatal:
		return err
	case <-c.Online:
		return nil
	case <-timer.C:
		return localization.New("cli.connection_timed_out")
	}
}

func printChats(db *archive.Store, out io.Writer, groupsOnly ...bool) error {
	language, languageErr := db.ApplicationLanguage()
	if languageErr != nil {
		return languageErr
	}
	if localized, ok := out.(*checkedOutput); ok && localization.Valid(localized.language) {
		language = localized.language
	}
	return printChatsLanguage(db, out, language, groupsOnly...)
}
func printChatsLanguage(db *archive.Store, out io.Writer, language string, groupsOnly ...bool) error {
	chats, err := db.Chats()
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	output := &checkedOutput{writer: table}
	output.println(localization.Text(language, "cli.id_type_name_tracked", nil))
	for _, c := range chats {
		group := strings.HasSuffix(c.ID, "@g.us")
		if len(groupsOnly) > 0 && groupsOnly[0] && !group {
			continue
		}
		kind := localization.Text(language, "cli.contact", nil)
		if group {
			kind = localization.Text(language, "cli.group", nil)
		}
		output.printf("%s\t%s\t%s\t%t\n", c.ID, kind, chatLabelLanguage(c, language), c.Tracked)
	}
	return errors.Join(output.failure(), table.Flush())
}
func printStatus(db *archive.Store, out io.Writer, zone *time.Location) error {
	language, languageErr := db.ApplicationLanguage()
	if languageErr != nil {
		return languageErr
	}
	if localized, ok := out.(*checkedOutput); ok && localization.Valid(localized.language) {
		language = localized.language
	}
	return printStatusLanguage(db, out, zone, language)
}
func printStatusLanguage(db *archive.Store, out io.Writer, zone *time.Location, language string) error {
	output := &checkedOutput{writer: out}
	chats, err := db.Chats()
	if err != nil {
		return err
	}
	for _, c := range chats {
		if !c.Tracked {
			continue
		}
		_, _, attempts, err := db.Snapshot(c.ID)
		if err != nil {
			return err
		}
		output.printf(localization.Text(language, "cli.s_s_messages_d_first_s_last_s_file", nil), chatLabelLanguage(c, language), c.ID, c.Count, formatTime(c.First, zone), formatTime(c.Last, zone), c.File)
		output.println(localization.Text(language, "cli.complete_history_and_absence_of_gaps_unconfirmed", nil))
		for _, a := range attempts {
			output.printf("  %s | %s | %s\n", formatTime(a.Started, zone), a.State, attemptDiagnostic(language, a))
		}
	}
	return output.failure()
}
func chatLabelLanguage(c archive.Chat, language string) string {
	if name := strings.TrimSpace(safe(c.Name)); name != "" {
		return name
	}
	if strings.HasSuffix(c.ID, "@g.us") {
		return localization.Text(language, "cli.group_name_unavailable", nil)
	}
	return localization.Text(language, "cli.contact_name_unavailable", nil)
}
func formatTime(t time.Time, z *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(z).Format(time.RFC3339)
}
func safe(s string) string {
	var out []rune
	for _, r := range s {
		if r >= 32 && r != 127 {
			out = append(out, r)
		}
	}
	return string(out)
}

// A joined shutdown failure must not disappear merely because Ctrl+C is present.
func onlyCancellation(err error) bool {
	if err == nil {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			if !onlyCancellation(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return onlyCancellation(child)
		}
	}
	return errors.Is(err, context.Canceled)
}

func eventLockGroup(command, group string) string {
	if command == "events" || command == "batch" {
		return "events:" + group
	}
	return group
}

func displayDiagnostic(language, value string) string {
	if localization.Known(value) {
		return localization.Text(language, value, nil)
	}
	return value
}

func attemptDiagnostic(language string, attempt archive.Attempt) string {
	if attempt.DetailCode != "" {
		return localization.Text(language, attempt.DetailCode, nil)
	}
	return attempt.Detail
}
