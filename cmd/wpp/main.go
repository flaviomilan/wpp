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
		_, _ = fmt.Fprintln(os.Stderr, "Erro:", err)
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
	writer io.Writer
	mu     sync.Mutex
	err    error
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
	output, diagnostic := &checkedOutput{writer: out}, &checkedOutput{writer: stderr}
	defer func() { resultErr = errors.Join(resultErr, output.failure(), diagnostic.failure()) }()
	flags := flag.NewFlagSet("wpp", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	data := flags.String("data-dir", "./data", "diretório de sessão e banco")
	exports := flags.String("export-dir", "./exports", "diretório dos arquivos JSONL")
	timezone := flags.String("timezone", "America/Sao_Paulo", "fuso horário de exibição no painel e status")
	listen := flags.String("listen", "127.0.0.1:8080", "endereço local da interface web")
	audioConfig := transcription.Config{}
	flags.StringVar(&audioConfig.Model, "whisper-model", "", "modelo ggml multilíngue; habilita transcrição durante run")
	flags.StringVar(&audioConfig.Whisper, "whisper-bin", "whisper-cli", "executável whisper.cpp")
	flags.StringVar(&audioConfig.FFmpeg, "ffmpeg-bin", "ffmpeg", "executável ffmpeg")
	flags.StringVar(&audioConfig.Language, "audio-language", "pt", "idioma dos áudios; auto para detectar")
	flags.StringVar(&audioConfig.Prompt, "audio-vocabulary", "", "nomes e termos específicos para orientar reconhecimento")
	flags.IntVar(&audioConfig.Threads, "audio-threads", 0, "threads de transcrição; 0 usa até 8 CPUs")
	flags.DurationVar(&audioConfig.Timeout, "audio-timeout", 30*time.Minute, "limite por áudio, sem truncar conteúdo")
	flags.Usage = func() {
		diagnostic.println("Uso: wpp [opções globais] <comando>\nComandos: login, chats [--groups], track <id>, untrack <id>, run, status, export, serve, audio status|retry <fonte>, audio correct <fonte> <transcricao.json>\nEventos JSONL: events next|status <grupo>, events ack <grupo> <lote>\nJSONL offline: events next|status <grupo>, events ack <grupo> <lote>, evidence <grupo> <fonte>; batch é alias de events")
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
		return errors.New("informe um comando")
	}
	command := rest[0]
	switch command {
	case "audio":
		if len(rest) == 4 && rest[1] == "correct" {
			break
		}
		if (len(rest) == 2 && rest[1] == "status") || (len(rest) == 3 && rest[1] == "retry") {
			break
		}
		return errors.New("uso: wpp audio status, wpp audio retry <fonte> ou wpp audio correct <fonte> <transcricao.json>")
	case "events", "batch":
		if len(rest) < 3 {
			return fmt.Errorf("uso: wpp %s next|status <grupo> ou %s ack <grupo> <lote>", command, command)
		}
		if !strings.HasSuffix(rest[2], "@g.us") {
			return errors.New("use um identificador de grupo @g.us")
		}
		if (rest[1] == "ack" && len(rest) != 4) || ((rest[1] == "next" || rest[1] == "status") && len(rest) != 3) || (rest[1] != "ack" && rest[1] != "next" && rest[1] != "status") {
			return fmt.Errorf("subcomando ou argumentos de %s inválidos", command)
		}

	case "evidence":
		if len(rest) != 3 {
			return errors.New("uso: wpp evidence <grupo> <fonte>")
		}

	case "track", "untrack":
		if len(rest) != 2 {
			return errors.New("uso: wpp " + command + " <id>")
		}
		j, err := types.ParseJID(rest[1])
		if err != nil || !whatsapp.Supported(j) || j.User == "" || j.Device != 0 {
			return errors.New("use o ID completo de um grupo ou contato exibido por wpp chats")
		}
	case "chats":
		if len(rest) > 2 || (len(rest) == 2 && rest[1] != "--groups") {
			return errors.New("uso: wpp chats [--groups]")
		}
	case "login", "run", "status", "export", "serve":
		if len(rest) != 1 {
			return errors.New("argumentos inesperados")
		}
	default:
		return fmt.Errorf("comando desconhecido: %s", command)
	}
	zone, err := time.LoadLocation(*timezone)
	if err != nil {
		return errors.New("fuso horário inválido")
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
	} else if command != "status" && command != "serve" && command != "run" && command != "audio" {
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
				return errors.Join(errors.New("transcrição JSON excede 16 MiB"), file.Close())
			}
			decoder := json.NewDecoder(io.LimitReader(file, 16*1024*1024+1))
			decoder.DisallowUnknownFields()
			e = decoder.Decode(&input)
			var extra any
			if e == nil {
				if next := decoder.Decode(&extra); !errors.Is(next, io.EOF) {
					e = errors.New("informe um único objeto JSON de transcrição")
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
		output.println("Fila de transcrição:", summary)
		for _, job := range jobs {
			output.printf("%s · %s · %s · tentativas %d · %s\n", job.Source, job.Chat, job.State, job.Attempts, job.Error)
		}
		output.println("Até 100 registros recentes. Consulte todas as páginas na interface.")
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
				output.println("Painel de controle:", url)
				output.println("Fechar a aba mantém o processamento. Ctrl+C encerra o painel e a captura.")
			}, zone, controller)
		}
		state, e := controller.Snapshot()
		if e != nil {
			return e
		}
		if !state.Owner {
			return service.ErrReadOnly
		}
		if !state.Linked {
			return errors.New("execute wpp login primeiro")
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
			return errors.New("configure ao menos uma conversa com wpp track <id>")
		}
		if e = controller.SetCapture(true); e != nil {
			return e
		}
		output.println("Captura solicitada. Ctrl+C encerra e conclui as exportações pendentes.")
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
				output.println("Nenhum evento novo.")
			} else {
				output.println(path)
			}
			return nil
		case "ack":
			if e := batches.Ack(rest[2], rest[3]); e != nil {
				return e
			}
			output.println("Lote confirmado:", rest[3])
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
		output.println("Acompanhamento atualizado:", rest[1])
		return exporter.ExportContext(ctx, false)
	case "status":
		return printStatus(db, out, zone)
	case "export":
		if err = exporter.ExportContext(ctx, true); err != nil {
			return err
		}
		output.println("Arquivos JSONL reconstruídos em", *exports)
		return nil
	}

	open := options.open
	if open == nil {
		open = openCLIConnection
	}
	client, err := open(ctx, *data, db, func(message string) { diagnostic.println(message) })
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, client.close()) }()
	if command == "login" {
		if client.account() != nil {
			if err = db.BindAccount(client.account().ToNonAD().String()); err != nil {
				return err
			}
			output.println("Sessão já vinculada. Execute wpp chats ou wpp run.")
			return nil
		}
		qr, err := client.qr(ctx)
		if err != nil {
			return err
		}
		if err = client.connect(ctx); err != nil {
			return errors.New("não foi possível conectar ao WhatsApp")
		}
		output.println("No celular: WhatsApp → Dispositivos conectados → Conectar dispositivo.")
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-client.events.Fatal:
				return err
			case item, ok := <-qr:
				if !ok {
					return errors.New("pareamento encerrado; tente wpp login novamente")
				}
				if item.Event == "code" {
					qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, output)
				}
				if item.Event == "success" {
					if err = waitOnline(ctx, client.events); err != nil {
						return err
					}
					output.println("Conta vinculada. Aguardando a sincronização inicial por 10 segundos.")
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
						diagnostic.println("Descoberta incompleta; execute wpp chats novamente.")
					}
					return nil
				}
				if item.Event == "timeout" || item.Event == "error" {
					return errors.New("pareamento não concluído; tente novamente")
				}
			}
		}
	}
	if client.account() == nil {
		return errors.New("execute wpp login primeiro")
	}
	if err = db.BindAccount(client.account().ToNonAD().String()); err != nil {
		return err
	}

	client.reconnect()
	if err = client.connect(ctx); err != nil {
		return errors.New("não foi possível conectar ao WhatsApp")
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
		return printChats(db, out, len(rest) == 2)
	}
	return errors.New("comando inválido")
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
		return errors.New("tempo limite de conexão excedido")
	}
}

func printChats(db *archive.Store, out io.Writer, groupsOnly ...bool) error {
	chats, err := db.Chats()
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	output := &checkedOutput{writer: table}
	output.println("ID\tTIPO\tNOME\tACOMPANHANDO")
	for _, c := range chats {
		group := strings.HasSuffix(c.ID, "@g.us")
		if len(groupsOnly) > 0 && groupsOnly[0] && !group {
			continue
		}
		kind := "contato"
		if group {
			kind = "grupo"
		}
		output.printf("%s\t%s\t%s\t%t\n", c.ID, kind, chatLabel(c), c.Tracked)
	}
	return errors.Join(output.failure(), table.Flush())
}
func printStatus(db *archive.Store, out io.Writer, zone *time.Location) error {
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
		output.printf("%s (%s)\n  Mensagens: %d | Primeira: %s | Última: %s\n  Arquivo: %s\n", chatLabel(c), c.ID, c.Count, formatTime(c.First, zone), formatTime(c.Last, zone), c.File)
		output.println("  Histórico integral e ausência de lacunas: não confirmados.")
		for _, a := range attempts {
			output.printf("  %s | %s | %s\n", formatTime(a.Started, zone), a.State, a.Detail)
		}
	}
	return output.failure()
}
func chatLabel(c archive.Chat) string {
	if name := strings.TrimSpace(safe(c.Name)); name != "" {
		return name
	}
	if strings.HasSuffix(c.ID, "@g.us") {
		return "Grupo (nome indisponível)"
	}
	return "Contato (nome indisponível)"
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
		return onlyCancellation(wrapped.Unwrap())
	}
	return errors.Is(err, context.Canceled)
}

func eventLockGroup(command, group string) string {
	if command == "events" || command == "batch" {
		return "events:" + group
	}
	return group
}
