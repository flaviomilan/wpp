package archive

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AtomicWrite publishes complete bytes without replacing a working export on failure.
func AtomicWrite(path string, content []byte) error {
	return atomicWriteStream(path, func(w io.Writer) error { _, err := w.Write(content); return err })
}
func atomicWriteStream(path string, write func(io.Writer) error) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".wpp-*.tmp")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer func() {
		if cleanupErr := os.Remove(temporary); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			err = errors.Join(err, cleanupErr)
		}
	}()
	defer func() {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	buffer := bufio.NewWriterSize(f, 64*1024)
	if err = write(buffer); err != nil {
		return err
	}
	if err = buffer.Flush(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	err = f.Close()
	f = nil
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	return directory.Sync()
}

type exportWriter struct {
	writer io.Writer
	ctx    context.Context
	err    error
}

func (w *exportWriter) Write(p []byte) (int, error) {
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}
func (w *exportWriter) WriteString(text string) (int, error) {
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	if w.err != nil {
		return 0, w.err
	}
	if writer, ok := w.writer.(io.StringWriter); ok {
		n, err := writer.WriteString(text)
		if err == nil && n != len(text) {
			err = io.ErrShortWrite
		}
		w.err = err
		return n, err
	}
	return w.Write([]byte(text))
}

type Exporter struct {
	Store *Store
	Dir   string
}

func (e Exporter) ExportContext(ctx context.Context, all bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.Store.migrateEvents(); err != nil {
		return err
	}
	chats, err := e.Store.Chats()
	if err != nil {
		return err
	}
	var failures []string
	for _, c := range chats {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !c.Tracked && c.Count == 0 {
			continue
		}
		path := filepath.Join(e.Dir, c.File)
		_, statErr := os.Stat(path)
		if !all && c.Generation == c.Exported && statErr == nil {
			continue
		}
		if err = e.ExportChatContext(ctx, c.ID); err != nil {
			if ctx.Err() != nil {
				return errors.Join(ctx.Err(), err)
			}
			failures = append(failures, "exportação falhou para "+c.ID)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}
func (e Exporter) ExportChatContext(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.Store.migrateEvents(); err != nil {
		return err
	}
	// Resolve the generated filename without holding the capture store lock.
	reader, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: e.Store.path}).String()+"?mode=ro&_txlock=deferred&_busy_timeout=5000")
	if err != nil {
		return err
	}
	var file string
	err = reader.QueryRowContext(ctx, "SELECT file FROM chats WHERE id=COALESCE((SELECT canonical FROM aliases WHERE alias=?),?)", id, id).Scan(&file)
	closeErr := reader.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	var c Chat
	if err = atomicWriteStream(filepath.Join(e.Dir, file), func(w io.Writer) error {
		var writeErr error
		c, writeErr = e.Store.writeJSONL(ctx, w, id, "")
		if writeErr == nil && c.File != file {
			return errors.New("identidade da conversa mudou durante a exportação; tente novamente")
		}
		return writeErr
	}); err != nil {
		return err
	}
	retired, err := e.Store.RetiredFiles(c.ID)
	if err != nil {
		return err
	}
	for _, file := range retired {
		// Only remove generated filenames, never paths provided by chat names.
		if !generatedExportName(file) {
			return fmt.Errorf("nome de exportação inválido")
		}
		if strings.HasSuffix(file, ".md") {
			continue
		} // Legacy files are retained; only JSONL is managed now.
		if err = os.Remove(filepath.Join(e.Dir, file)); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = e.Store.ForgetRetired(file); err != nil {
			return err
		}
	}
	return e.Store.MarkExported(c.ID, c.Generation)
}

type pendingExport struct {
	generation int64
	changed    time.Time
}
type Debouncer struct {
	Exporter Exporter
	Delay    time.Duration
	pending  map[string]pendingExport
}

func (d *Debouncer) TickContext(ctx context.Context, now time.Time) error {
	if d.pending == nil {
		d.pending = map[string]pendingExport{}
	}
	chats, err := d.Exporter.Store.Chats()
	if err != nil {
		return err
	}
	var failures []string
	for _, c := range chats {
		if !c.Tracked && c.Count == 0 {
			continue
		}
		_, statErr := os.Stat(filepath.Join(d.Exporter.Dir, c.File))
		if c.Generation == c.Exported && statErr == nil {
			delete(d.pending, c.ID)
			continue
		}
		p, exists := d.pending[c.ID]
		if !exists || p.generation != c.Generation {
			d.pending[c.ID] = pendingExport{generation: c.Generation, changed: now}
			continue
		}
		if now.Sub(p.changed) < d.Delay {
			continue
		}
		if err = d.Exporter.ExportChatContext(ctx, c.ID); err != nil {
			if ctx.Err() != nil {
				return errors.Join(ctx.Err(), err)
			}
			failures = append(failures, "exportação falhou para "+c.ID)
			p.changed = now
			d.pending[c.ID] = p
		} else {
			delete(d.pending, c.ID)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func generatedExportName(file string) bool {
	suffix := ".jsonl"
	if strings.HasSuffix(file, ".md") {
		suffix = ".md"
	}
	if len(file) != 5+64+len(suffix) || !strings.HasPrefix(file, "chat-") || !strings.HasSuffix(file, suffix) {
		return false
	}
	_, err := hex.DecodeString(file[5:69])
	return err == nil
}
