package service

import (
	"context"
	"errors"
	"sync"
	"wpp/internal/archive"
	"wpp/internal/transcription"
)

type audioCoordinator struct {
	mu                          sync.Mutex
	configMu                    sync.Mutex
	queue                       workQueue
	initialReady                chan struct{}
	workspaceReady              chan struct{}
	engine                      transcription.Engine
	config                      transcription.Config
	engineError, workspaceError string
	workspace                   *transcription.Workspace
	source, phase               string
	manual                      int
	closeErr                    error
}

func newAudio() *audioCoordinator {
	return &audioCoordinator{initialReady: make(chan struct{}), workspaceReady: make(chan struct{})}
}
func (a *audioCoordinator) setEngine(engine transcription.Engine, cfg transcription.Config) {
	a.mu.Lock()
	a.engine = engine
	a.config = cfg
	a.engineError = ""
	a.mu.Unlock()
}
func (a *audioCoordinator) engineFailed() {
	a.mu.Lock()
	a.engineError = "Transcrição indisponível. Confira o modelo e os executáveis em Ajustes."
	a.mu.Unlock()
}
func (a *audioCoordinator) engineAvailable() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine != nil
}
func (a *audioCoordinator) awaitEngine(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.initialReady:
		return nil
	}
}
func (a *audioCoordinator) manualRequest() func() {
	a.mu.Lock()
	a.manual++
	a.mu.Unlock()
	return func() { a.mu.Lock(); a.manual--; a.mu.Unlock() }
}
func (a *audioCoordinator) automaticAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine != nil && a.workspace != nil && a.workspaceError == "" && a.manual == 0
}
func (a *audioCoordinator) prepare(dir string, store *archive.Store) error {
	workspace, err := transcription.OpenWorkspace(dir, store)
	a.mu.Lock()
	a.workspace = workspace
	if err != nil {
		a.workspaceError = "Não foi possível preparar a fila. Confira permissões, espaço e processos no diretório de dados."
	}
	a.mu.Unlock()
	close(a.workspaceReady)
	return err
}
func (a *audioCoordinator) snapshot() (source, phase, detail string, available bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	detail = a.engineError
	if a.workspaceError != "" {
		detail = a.workspaceError
	}
	return a.source, a.phase, detail, a.engine != nil && a.workspace != nil && a.workspaceError == ""
}
func (a *audioCoordinator) run(ctx context.Context, store *archive.Store, t Transport, source string, progress func(string) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.workspaceReady:
	}
	a.mu.Lock()
	workspace, engine, cfg := a.workspace, a.engine, a.config
	a.mu.Unlock()
	if workspace == nil {
		return invalid("A fila de áudio não está disponível. Confira Ajustes.")
	}
	if engine == nil {
		return nil
	}
	defer func() { a.mu.Lock(); a.source = ""; a.phase = ""; a.mu.Unlock() }()
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var progressErr error
	worker := transcription.Worker{Store: store, Download: t, Engine: engine, Timeout: cfg.Timeout, Source: source, Phase: func(job archive.AudioJob, phase string) {
		a.mu.Lock()
		a.source = job.Source
		a.phase = phase
		a.mu.Unlock()
		if progressErr == nil && progress != nil {
			progressErr = progress(phase)
			if progressErr != nil {
				cancel()
			}
		}
	}}
	_, err := worker.ProcessOne(jobCtx, workspace.Root)
	if err != nil && !persistenceFailure(err) && !errors.Is(err, context.Canceled) {
		a.mu.Lock()
		a.workspaceError = "Falha na área temporária dos áudios. Confira espaço e permissões e reinicie o painel."
		a.mu.Unlock()
	}
	return errors.Join(err, progressErr)
}
func (a *audioCoordinator) close() error {
	a.mu.Lock()
	workspace := a.workspace
	a.workspace = nil
	a.mu.Unlock()
	if workspace != nil {
		a.closeErr = workspace.Close()
	}
	return a.closeErr
}
