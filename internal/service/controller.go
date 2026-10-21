package service

import (
	"context"
	"encoding/json"
	"errors"
	"go.mau.fi/whatsmeow/types"
	"path/filepath"
	"rsc.io/qr"
	"sync"
	"time"
	"wpp/internal/archive"
	"wpp/internal/localization"
	"wpp/internal/transcription"
	"wpp/internal/whatsapp"
)

var ErrReadOnly = localization.New("backend.another_capture_process_controls_this_directory_stop_it_and_reopen")

type Settings struct {
	Capture       bool   `json:"capture_enabled"`
	Transcription bool   `json:"transcription_enabled"`
	Whisper       string `json:"whisper_path"`
	FFmpeg        string `json:"ffmpeg_path"`
	Model         string `json:"model_path"`
	Language      string `json:"language"`
	Vocabulary    string `json:"vocabulary"`
	Threads       int    `json:"threads"`
	Timeout       string `json:"timeout"`
}

func (s Settings) audioConfig() (transcription.Config, error) {
	timeout, e := time.ParseDuration(s.Timeout)
	if e != nil || timeout <= 0 {
		return transcription.Config{}, invalid("backend.enter_a_positive_time_limit_such_as_30m")
	}
	if !transcription.ValidLanguage(s.Language) || s.Threads < 0 || s.Threads > 256 || len(s.Vocabulary) > 2048 {
		return transcription.Config{}, invalid("backend.check_language_threads_0_to_256_and_vocabulary_up_to")
	}
	return transcription.Config{Whisper: s.Whisper, FFmpeg: s.FFmpeg, Model: s.Model, Language: s.Language, Prompt: s.Vocabulary, Threads: s.Threads, Timeout: timeout}, nil
}

type Options struct {
	DataDir, ExportDir string
	Zone               *time.Location
	Factory            Factory
	Audio              *transcription.Config
	EngineFactory      func(context.Context, transcription.Config) (transcription.Engine, error)
}
type Snapshot struct {
	Owner           bool                `json:"owner"`
	Linked          bool                `json:"linked"`
	Connection      string              `json:"connection"`
	ErrorCode       string              `json:"error_code,omitempty"`
	AudioErrorCode  string              `json:"audio_error_code,omitempty"`
	ExportErrorCode string              `json:"export_error_code,omitempty"`
	Error           string              `json:"error,omitempty"`
	Settings        Settings            `json:"settings"`
	AudioSource     string              `json:"audio_source,omitempty"`
	AudioPhase      string              `json:"audio_phase,omitempty"`
	AudioError      string              `json:"audio_error,omitempty"`
	ExportError     string              `json:"export_error,omitempty"`
	QRAvailable     bool                `json:"qr_available"`
	Operations      []archive.Operation `json:"operations"`
	HistoryRunning  bool                `json:"history_running"`
	AudioAvailable  bool                `json:"audio_available"`
}
type Controller struct {
	store                 *archive.Store
	opts                  Options
	ctx                   context.Context
	cancel                context.CancelFunc
	mu                    sync.Mutex
	wg                    sync.WaitGroup
	closed                bool
	unlock                func()
	owner                 bool
	settings              Settings
	connection, errorText string
	blocked               bool
	operations            *operationManager
	storageError          error
	connections           *connectionCoordinator
	history               historyCoordinator
	exports               *exportCoordinator
	audio                 *audioCoordinator
}

func New(ctx context.Context, store *archive.Store, opts Options) (*Controller, error) {
	if opts.Zone == nil {
		opts.Zone = time.UTC
	}
	if opts.Factory == nil {
		opts.Factory = openWhatsApp
	}
	if opts.EngineFactory == nil {
		opts.EngineFactory = func(ctx context.Context, c transcription.Config) (transcription.Engine, error) {
			return transcription.NewLocal(ctx, c)
		}
	}
	root := filepath.Dir(opts.DataDir)
	defaults := Settings{Whisper: filepath.Join(root, ".tools/audio/whisper/build/bin/whisper-cli"), Model: filepath.Join(root, ".tools/audio/models/ggml-large-v3-turbo.bin"), FFmpeg: "ffmpeg", Language: "pt", Timeout: "30m", Transcription: true}
	raw, _ := json.Marshal(defaults)
	raw, e := store.ControlSettings(raw)
	if e != nil {
		return nil, e
	}
	var settings Settings
	if e = json.Unmarshal(raw, &settings); e != nil {
		return nil, e
	}
	if opts.Audio != nil {
		a := opts.Audio
		settings.Whisper = a.Whisper
		settings.FFmpeg = a.FFmpeg
		settings.Model = a.Model
		settings.Language = a.Language
		settings.Vocabulary = a.Prompt
		settings.Threads = a.Threads
		settings.Timeout = a.Timeout.String()
		settings.Transcription = true
	}
	workerCtx, cancel := context.WithCancel(ctx)
	c := &Controller{store: store, opts: opts, ctx: workerCtx, cancel: cancel, settings: settings, connection: "disconnected"}
	c.operations = newOperations(workerCtx, store, c.storageFailure)
	c.connections = newConnection(workerCtx, opts, store, c.onConnectionEvent)
	c.exports = newExports(store, opts)
	c.audio = newAudio()
	c.unlock, e = archive.Lock(opts.DataDir)
	c.owner = e == nil
	if c.owner {
		if e = store.InterruptOperations(); e != nil {
			c.unlock()
			cancel()
			return nil, e
		}
		if e = c.connections.open(); e != nil {
			c.errorText = e.Error()
		}
		c.spawn(c.background)
		c.spawn(c.audioBackground)
		c.spawn(c.exportsBackground)
		c.spawn(func() {
			c.audio.configMu.Lock()
			defer c.audio.configMu.Unlock()
			defer close(c.audio.initialReady)
			// loadEngine records a public failure in the audio state. Capture must
			// remain available when the configured engine cannot start.
			_ = c.loadEngine(c.ctx, settings)
		})
	} else {
		c.connection = "external"
		c.errorText = "backend.another_capture_process_controls_this_directory_stop_it_and_reopen"
	}
	return c, nil
}
func (c *Controller) spawn(fn func()) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go func() { defer c.wg.Done(); fn() }()
}
func (c *Controller) onConnectionEvent(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch event.Kind {
	case "online":
		if c.storageError == nil {
			c.connection = ""
			c.errorText = ""
			c.blocked = false
		}
		c.history.request()
	case "offline":
		c.history.pause()
	case "needs_login", "error":
		c.connection = event.Kind
		c.blocked = true
		if event.Kind == "needs_login" {
			c.errorText = "backend.session_revoked_link_again_using_the_qr_code"
		} else {
			c.errorText = "backend.whatsapp_disconnected_try_connecting_again"
		}
		c.history.pause()
	case "activity":
		c.history.seedObserved(event.Chat)
	}
}
func (c *Controller) Snapshot() (Snapshot, error) {
	ops, e := c.operations.snapshot()
	if e != nil {
		c.storageFailure(e)
	}
	status, detail, linked := c.connections.snapshot()
	audioSource, audioPhase, audioError, audioAvailable := c.audio.snapshot()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errorText != "" {
		detail = c.errorText
		if c.connection != "" {
			status = c.connection
		}
	}
	return Snapshot{Owner: c.owner, Linked: linked, Connection: status, Error: localMessage(detail), ErrorCode: localization.CodeForKey(detail), Settings: c.settings, AudioSource: audioSource, AudioPhase: audioPhase, AudioError: localMessage(audioError), AudioErrorCode: localization.CodeForKey(audioError), ExportError: localMessage(c.exports.status()), ExportErrorCode: localization.CodeForKey(c.exports.status()), QRAvailable: len(c.connections.QR()) > 0, Operations: ops, HistoryRunning: c.history.active(), AudioAvailable: audioAvailable}, nil
}
func (c *Controller) QR() []byte { return c.connections.QR() }
func (c *Controller) saveSettingsLocked() error {
	raw, e := json.Marshal(c.settings)
	if e != nil {
		return e
	}
	if err := c.store.SaveControlSettings(raw); err != nil {
		c.storageFailureLocked(err)
		return &archive.PersistenceError{Err: err}
	}
	return nil
}
func (c *Controller) SetCapture(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.owner {
		return ErrReadOnly
	}
	if c.closed {
		return conflict("backend.the_panel_is_shutting_down")
	}
	previous := c.settings.Capture
	c.settings.Capture = on
	if e := c.saveSettingsLocked(); e != nil {
		c.settings.Capture = previous
		return e
	}
	if on {
		c.blocked = false
		c.storageError = nil
		c.errorText = ""
		c.history.request()
	} else {
		c.history.pause()
	}
	return nil
}
func (c *Controller) SetTranscription(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.owner {
		return ErrReadOnly
	}
	if c.closed {
		return conflict("backend.the_panel_is_shutting_down")
	}
	previous := c.settings.Transcription
	c.settings.Transcription = on
	if e := c.saveSettingsLocked(); e != nil {
		c.settings.Transcription = previous
		return e
	}
	return nil
}
func validChat(id string) bool {
	j, e := types.ParseJID(id)
	return e == nil && j.User != "" && j.Device == 0 && whatsapp.Supported(j)
}
func (c *Controller) Track(id string, on bool) error {
	if !validChat(id) {
		return invalid("backend.invalid_chat_identifier")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.owner {
		return ErrReadOnly
	}
	if e := c.store.Track(id, on); e != nil {
		c.storageFailureLocked(e)
		return &archive.PersistenceError{Err: e}
	}
	if !on {
		c.operations.cancelChat(id)
		c.history.pause()
	}
	c.history.request()
	return nil
}
func (c *Controller) acquire(ctx context.Context, connect bool) (Transport, func(), error) {
	c.mu.Lock()
	blocked := c.blocked
	c.mu.Unlock()
	if blocked && connect {
		return nil, nil, invalid("backend.check_the_connection_and_storage_before_continuing")
	}
	return c.connections.acquire(ctx, connect)
}
func (c *Controller) closeIdle() {
	c.mu.Lock()
	keep := c.settings.Capture && !c.blocked
	c.mu.Unlock()
	if err := c.connections.idle(keep); err != nil && c.ctx.Err() == nil {
		c.mu.Lock()
		c.connection = "error"
		c.errorText = "backend.failed_to_close_the_connection"
		c.mu.Unlock()
	}
}
func (c *Controller) submit(kind, chat string, fn func(context.Context, *task) error) (archive.Operation, error) {
	return c.submitSource(kind, chat, "", fn)
}
func (c *Controller) submitSource(kind, chat, source string, fn func(context.Context, *task) error) (archive.Operation, error) {
	c.mu.Lock()
	owner, closed, failure := c.owner, c.closed, c.storageError
	c.mu.Unlock()
	if !owner {
		return archive.Operation{}, ErrReadOnly
	}
	if closed {
		return archive.Operation{}, conflict("backend.the_panel_is_shutting_down")
	}
	if failure != nil {
		return archive.Operation{}, &archive.PersistenceError{Err: failure}
	}
	return c.operations.submit(kind, chat, source, fn)
}
func (c *Controller) update(t *task, state, phase, detail string, total, completed int) error {
	return c.operations.update(t, state, phase, detail, total, completed)
}
func (c *Controller) Cancel(id string) error {
	c.mu.Lock()
	owner := c.owner
	c.mu.Unlock()
	if !owner {
		return ErrReadOnly
	}
	return c.operations.cancel(id)
}
func (c *Controller) Pair() (archive.Operation, error) {
	return c.submit("pair", "", func(ctx context.Context, task *task) error {
		t, release, e := c.acquire(ctx, false)
		if e != nil {
			return e
		}
		defer release()
		if err := c.update(task, "running", "pairing", "No celular, abra WhatsApp → Dispositivos conectados → Conectar dispositivo.", 0, 0); err != nil {
			return err
		}
		defer c.connections.setQR(nil)
		if !t.Session() {
			e = t.Pair(ctx, func(value string) {
				code, e := qr.Encode(value, qr.M)
				if e != nil {
					return
				}
				code.Scale = 6
				c.connections.setQR(code.PNG())
			})
			if e != nil {
				return e
			}
		}
		if e = t.Connect(ctx); e != nil {
			return e
		}
		c.mu.Lock()
		c.connections.markConnected()
		c.connection = ""
		c.blocked = false
		c.errorText = ""
		c.mu.Unlock()
		if err := c.update(task, "running", "discovering", "backend.account_connected_updating_chats", 0, 0); err != nil {
			return err
		}
		if e = t.Discover(ctx); e != nil {
			if err := c.update(task, "warning", "finished", "backend.account_connected_some_chats_have_not_been_updated_yet", 0, 0); err != nil {
				return err
			}
			return nil
		}
		if err := c.update(task, "running", "finished", "backend.account_connected_and_chats_updated", 0, 0); err != nil {
			return err
		}
		return nil
	})
}
func (c *Controller) Discover() (archive.Operation, error) {
	return c.submit("discover", "", func(ctx context.Context, task *task) error {
		t, release, e := c.acquire(ctx, true)
		if e != nil {
			return e
		}
		defer release()
		if err := c.update(task, "running", "discovering", "Atualizando grupos, contatos e nomes.", 0, 0); err != nil {
			return err
		}
		if e = t.Discover(ctx); e != nil {
			return invalid("backend.could_not_update_all_chats_try_again")
		}
		if err := c.update(task, "running", "finished", "Conversas atualizadas.", 0, 0); err != nil {
			return err
		}
		return nil
	})
}
func (c *Controller) Export(chat string) (archive.Operation, error) {
	if chat != "" && !validChat(chat) {
		return archive.Operation{}, invalid("backend.invalid_identifier")
	}
	return c.submit("export", chat, func(ctx context.Context, task *task) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if err := c.update(task, "running", "exporting", "Reconstruindo JSONL.", 0, 0); err != nil {
			return err
		}
		e := c.exports.exportContext(ctx, chat, true)
		if e != nil {
			return invalid("backend.export_failed_messages_remain_saved_in_sqlite")
		}
		if err := c.update(task, "running", "finished", "JSONL atualizado em "+c.opts.ExportDir+".", 0, 0); err != nil {
			return err
		}
		return nil
	})
}
func (c *Controller) Sync(chat string) (archive.Operation, error) {
	if !validChat(chat) {
		return archive.Operation{}, invalid("backend.invalid_identifier")
	}
	canonical, e := c.store.Canonical(chat)
	if e != nil {
		return archive.Operation{}, e
	}
	chat = canonical
	chats, e := c.store.Chats()
	if e != nil {
		return archive.Operation{}, e
	}
	var selected *archive.Chat
	for _, entry := range chats {
		if entry.ID == chat {
			copy := entry
			selected = &copy
		}
	}
	if selected == nil || !selected.Tracked {
		return archive.Operation{}, conflict("backend.track_chat_before_sync")
	}
	return c.submit("sync", chat, func(ctx context.Context, task *task) error {
		t, release, e := c.acquire(ctx, true)
		if e != nil {
			return e
		}
		defer release()
		if err := c.update(task, "running", "history", "backend.retrieving_history_available_on_the_phone", 0, 0); err != nil {
			return err
		}
		releaseHistory, waitErr := c.history.queue.acquire(ctx, true)
		if waitErr != nil {
			return waitErr
		}
		checkpoint, e := archive.Checkpoints(c.store)
		if e == nil {
			e = (&archive.Synchronizer{Store: c.store, Source: t, Delay: time.Second}).Recover(ctx, *selected, checkpoint[chat])
		}
		releaseHistory()
		var hist *archive.HistoryError
		if e != nil && (persistenceFailure(e) || !errors.As(e, &hist)) {
			return errors.Join(ctx.Err(), &archive.PersistenceError{Err: e})
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.mu.Lock()
		enabled := c.settings.Transcription
		engine := c.audio.engineAvailable()
		c.mu.Unlock()
		endManual := c.audio.manualRequest()
		defer endManual()
		if enabled && !engine {
			if err := c.audio.awaitEngine(ctx); err != nil {
				return err
			}
			c.mu.Lock()
			engine = c.audio.engineAvailable()
			c.mu.Unlock()
		}
		ids, err := c.store.AudioSnapshot(chat)
		if err != nil {
			return &archive.PersistenceError{Err: err}
		}
		completed := 0
		warnings := e != nil
		if enabled && engine {
			for _, source := range ids {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				c.mu.Lock()
				on := c.settings.Transcription
				c.mu.Unlock()
				if !on {
					warnings = true
					break
				}
				if err := c.update(task, "running", "audio", "backend.processing_audio_completed_of_total", len(ids), completed); err != nil {
					return err
				}
				releaseAudio, waitErr := c.audio.queue.acquire(ctx, true)
				if waitErr != nil {
					return waitErr
				}
				c.mu.Lock()
				on = c.settings.Transcription
				c.mu.Unlock()
				if !on {
					releaseAudio()
					warnings = true
					break
				}
				state, stateErr := c.store.AudioState(source)
				if stateErr != nil {
					releaseAudio()
					return &archive.PersistenceError{Err: stateErr}
				}
				if state == "pending" {
					err = c.processAudio(ctx, t, source)
				}
				releaseAudio()
				if err != nil {
					return err
				}
				state, stateErr = c.store.AudioState(source)
				if stateErr != nil {
					return &archive.PersistenceError{Err: stateErr}
				}
				if state != "done" {
					warnings = true
				}
				completed++
			}
		} else if len(ids) > 0 {
			warnings = true
		}
		// Missing descriptors and previous failures remain visible as pending evidence.
		counts, err := c.store.AudioSummaryFor(chat)
		if err != nil {
			return &archive.PersistenceError{Err: err}
		}
		if counts["waiting_metadata"]+counts["failed"]+counts["pending"]+counts["processing"] > 0 {
			warnings = true
		}
		state := "running"
		if warnings {
			state = "warning"
		}
		detail := "backend.operation_completed_history_coverage_remains_unconfirmed"
		if warnings {
			detail = "backend.operation_finished_with_outstanding_items_check_history_and_the_audio"
		}
		if err := c.update(task, state, "finished", detail, len(ids), completed); err != nil {
			return err
		}
		return nil
	})
}
func (c *Controller) loadEngine(ctx context.Context, settings Settings) error {
	cfg, e := settings.audioConfig()
	var engine transcription.Engine
	if e == nil {
		engine, e = c.opts.EngineFactory(ctx, cfg)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e != nil {
		c.audio.engineFailed()
		return e
	}
	c.audio.setEngine(engine, cfg)
	return nil
}
func (c *Controller) Configure(settings Settings) (archive.Operation, error) {
	return c.submit("settings", "", func(ctx context.Context, t *task) error {
		c.audio.configMu.Lock()
		defer c.audio.configMu.Unlock()
		cfg, e := settings.audioConfig()
		if e != nil {
			return e
		}
		if err := c.update(t, "running", "validating", "backend.validating_the_transcription_engine_and_model", 0, 0); err != nil {
			return err
		}
		engine, e := c.opts.EngineFactory(ctx, cfg)
		if e != nil {
			return invalid("backend.could_not_validate_settings_check_executables_model_language_and_limits")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if e = ctx.Err(); e != nil {
			return e
		}
		settings.Capture = c.settings.Capture
		settings.Transcription = c.settings.Transcription
		raw, _ := json.Marshal(settings)
		if e = c.store.SaveControlSettings(raw); e != nil {
			c.storageFailureLocked(e)
			return &archive.PersistenceError{Err: e}
		}
		c.settings = settings
		c.audio.setEngine(engine, cfg)
		return nil
	})
}
func (c *Controller) background() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
			c.mu.Lock()
			active := c.settings.Capture && !c.blocked && c.history.ready()
			c.mu.Unlock()
			if active {
				chats, e := c.store.Chats()
				if e != nil {
					c.storageFailure(e)
					continue
				}
				tracked := false
				for _, chat := range chats {
					tracked = tracked || chat.Tracked
					if chat.Tracked && chat.Seed == nil {
						c.mu.Lock()
						c.history.awaitSeed(chat.ID)
						c.mu.Unlock()
					}
				}
				if tracked {
					ctx, cancel, started := c.history.begin(c.ctx)
					if !started {
						continue
					}
					c.spawn(func() {
						defer cancel()
						defer c.history.finish()
						t, release, e := c.acquire(ctx, true)
						if e != nil {
							return
						}
						defer release()
						releaseHistory, waitErr := c.history.queue.acquire(ctx, false)
						if waitErr != nil {
							return
						}
						defer releaseHistory()
						checkpoints, e := archive.Checkpoints(c.store)
						if e != nil {
							c.storageFailure(e)
							return
						}
						s := archive.Synchronizer{Store: c.store, Source: t, Delay: time.Second}
						if e = s.All(ctx, checkpoints, func(error) {}); e != nil && ctx.Err() == nil {
							c.storageFailure(e)
						}
					})
				}
			}
			c.closeIdle()
		}
	}
}
func (c *Controller) processAudio(ctx context.Context, t Transport, source string) error {
	c.mu.Lock()
	failure := c.storageError
	c.mu.Unlock()
	if failure != nil {
		return &archive.PersistenceError{Err: failure}
	}
	return c.audio.run(ctx, c.store, t, source, func(phase string) error {
		if source == "" {
			return nil
		}
		return c.operations.progress(source, phase)
	})
}
func (c *Controller) audioBackground() {
	if err := c.audio.prepare(c.opts.DataDir, c.store); err != nil {
		if persistenceFailure(err) {
			c.storageFailure(err)
		}
		return
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
			c.mu.Lock()
			active := !c.blocked && c.settings.Capture && c.settings.Transcription
			c.mu.Unlock()
			if !active || !c.audio.automaticAllowed() || !c.connections.connected() {
				continue
			}
			releaseAudio, err := c.audio.queue.acquire(c.ctx, false)
			if err != nil {
				return
			}
			c.mu.Lock()
			active = !c.blocked && c.settings.Capture && c.settings.Transcription
			c.mu.Unlock()
			if active && c.audio.automaticAllowed() {
				t, release, err := c.acquire(c.ctx, true)
				if err == nil {
					c.mu.Lock()
					active = !c.blocked && c.settings.Capture && c.settings.Transcription
					c.mu.Unlock()
					if active {
						if err = c.processAudio(c.ctx, t, ""); persistenceFailure(err) {
							c.storageFailure(err)
						}
					}
					release()
				}
			}
			releaseAudio()
		}
	}
}
func (c *Controller) exportsBackground() {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case now := <-tick.C:
			c.exports.tick(c.ctx, now)
		}
	}
}
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	opErr := c.operations.close()
	c.wg.Wait()
	closeErr := c.connections.close()
	audioErr := c.audio.close()
	if c.owner {
		attemptErr := c.store.InterruptAttempts()
		exportErr := c.exports.export("", false)
		c.unlock()
		return errors.Join(opErr, closeErr, audioErr, attemptErr, exportErr)
	}
	return errors.Join(opErr, closeErr, audioErr)
}
