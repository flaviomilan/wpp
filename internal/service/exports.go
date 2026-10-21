package service

import (
	"context"
	"sync"
	"time"
	"wpp/internal/archive"
)

type exportCoordinator struct {
	mu       sync.Mutex
	exporter archive.Exporter
	debounce archive.Debouncer
	last     error
}

func newExports(store *archive.Store, opts Options) *exportCoordinator {
	e := archive.Exporter{Store: store, Dir: opts.ExportDir}
	return &exportCoordinator{exporter: e, debounce: archive.Debouncer{Exporter: e, Delay: 2 * time.Second}}
}
func (e *exportCoordinator) export(chat string, all bool) error {
	return e.exportContext(context.Background(), chat, all)
}
func (e *exportCoordinator) exportContext(ctx context.Context, chat string, all bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var err error
	if chat == "" {
		err = e.exporter.ExportContext(ctx, all)
	} else {
		err = e.exporter.ExportChatContext(ctx, chat)
	}
	if err != nil {
		e.last = err
	} else if chat == "" {
		e.last = nil
	}
	return err
}
func (e *exportCoordinator) tick(ctx context.Context, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.debounce.TickContext(ctx, now); err != nil {
		e.last = err
		return
	}
	if e.last != nil {
		chats, err := e.exporter.Store.Chats()
		if err != nil {
			e.last = err
			return
		}
		for _, chat := range chats {
			if (chat.Tracked || chat.Count > 0) && chat.Generation != chat.Exported {
				return
			}
		}
		e.last = nil
	}
}
func (e *exportCoordinator) status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last != nil {
		return "backend.jsonl_export_failed_check_the_export_directory_and_try_export"
	}
	return ""
}
