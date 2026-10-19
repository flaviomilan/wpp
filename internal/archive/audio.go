package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAudioOffline = errors.New("WhatsApp desconectado")

// Metadata never leaves the queue through the public browser API.
type AudioJob struct {
	Source      string `json:"source_id"`
	Chat        string `json:"chat"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	Error       string `json:"error,omitempty"`
	NextAttempt int64  `json:"next_attempt"`
	Metadata    []byte `json:"-"`
	Token       string `json:"-"`
	Phase       string `json:"phase,omitempty"`
}

func migrateAudio(tx *sql.Tx) error {
	_, err := tx.Exec(`
 CREATE TABLE IF NOT EXISTS audio_jobs(
 source_id TEXT PRIMARY KEY,chat TEXT NOT NULL,state TEXT NOT NULL,
 metadata BLOB,attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt INTEGER NOT NULL DEFAULT 0,lease_until INTEGER NOT NULL DEFAULT 0,
 token TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',updated INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS audio_ready ON audio_jobs(state,next_attempt);
 INSERT OR IGNORE INTO audio_jobs(source_id,chat,state,updated,error)
 SELECT source_id,chat,'waiting_metadata',strftime('%s','now'),'aguardando metadados do WhatsApp'
 FROM messages WHERE json_extract(data,'$.Kind')='áudio' AND COALESCE(json_extract(data,'$.Deleted'),0)=0;
 PRAGMA user_version=4;`)
	return err
}

func saveAudio(tx *sql.Tx, source, preferred string, m Message, metadata []byte) error {
	now := time.Now().UTC().Unix()
	if preferred != "" && preferred != source {
		if _, err := tx.Exec("INSERT INTO audio_jobs(source_id,chat,state,metadata,attempts,next_attempt,lease_until,token,error,updated) SELECT ?,chat,state,metadata,attempts,next_attempt,lease_until,token,error,updated FROM audio_jobs WHERE source_id=? ON CONFLICT(source_id) DO NOTHING", source, preferred); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM audio_jobs WHERE source_id=?", preferred); err != nil {
			return err
		}
	}
	if m.Deleted || m.Kind != "áudio" {
		_, err := tx.Exec("UPDATE audio_jobs SET state='cancelled',metadata=NULL,token='',lease_until=0,error='mensagem excluída ou substituída',updated=? WHERE source_id=? AND state<>'done'", now, source)
		return err
	}
	if m.Transcript != nil {
		_, err := tx.Exec("INSERT INTO audio_jobs(source_id,chat,state,updated) VALUES(?,?,'done',?) ON CONFLICT(source_id) DO UPDATE SET chat=excluded.chat,state='done',metadata=NULL,token='',lease_until=0,error='',updated=excluded.updated", source, m.Chat, now)
		return err
	}
	state := "waiting_metadata"
	detail := "aguardando metadados do WhatsApp"
	if len(metadata) > 0 {
		state = "pending"
		detail = ""
	}
	_, err := tx.Exec("INSERT INTO audio_jobs(source_id,chat,state,metadata,updated,error) VALUES(?,?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET chat=excluded.chat,metadata=CASE WHEN audio_jobs.state<>'done' AND excluded.metadata IS NOT NULL THEN excluded.metadata ELSE audio_jobs.metadata END,state=CASE WHEN audio_jobs.state='waiting_metadata' AND excluded.metadata IS NOT NULL THEN 'pending' ELSE audio_jobs.state END,error=CASE WHEN audio_jobs.state='waiting_metadata' AND excluded.metadata IS NOT NULL THEN '' ELSE audio_jobs.error END", source, m.Chat, state, nullableMetadata(metadata), now, detail)
	return err
}
func nullableMetadata(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func (s *Store) AudioJobs(offset, limit int) ([]AudioJob, error) {
	return s.AudioJobsFiltered(offset, limit, "", "")
}
func (s *Store) AudioJobsFiltered(offset, limit int, chat, state string) ([]AudioJob, error) {
	if offset < 0 || limit < 1 || limit > 101 {
		return nil, errors.New("paginação inválida")
	}
	jobs := []AudioJob{}
	err := s.transaction(func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT source_id,chat,state,attempts,error,next_attempt,phase FROM audio_jobs WHERE (?='' OR chat=?) AND (?='' OR state=?) ORDER BY updated DESC,source_id LIMIT ? OFFSET ?", chat, chat, state, state, limit, offset)
		if err != nil {
			return err
		}
		// Rows.Err reports errors from automatic closure at the end of iteration.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var j AudioJob
			if err = rows.Scan(&j.Source, &j.Chat, &j.State, &j.Attempts, &j.Error, &j.NextAttempt, &j.Phase); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, err
}
func (s *Store) AudioSummary() (map[string]int, error) {
	return s.AudioSummaryFor("")
}
func (s *Store) AudioSummaryFor(chat string) (map[string]int, error) {
	out := map[string]int{}
	err := s.transaction(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT state,COUNT(*) FROM audio_jobs WHERE (?='' OR chat=?) GROUP BY state", chat, chat)
		if e != nil {
			return e
		}
		// Rows.Err reports errors from automatic closure at the end of iteration.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var state string
			var n int
			if e = rows.Scan(&state, &n); e != nil {
				return e
			}
			out[state] = n
		}
		return rows.Err()
	})
	return out, err
}

// RecoverAudioWorker is called only while holding the exclusive AudioLock.
func (s *Store) RecoverAudioWorker() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE audio_jobs SET state='pending',attempts=MAX(0,attempts-1),next_attempt=0,token='',lease_until=0,error='processamento interrompido',updated=? WHERE state='processing'", time.Now().Unix())
		return err
	})
}
func (s *Store) ClaimAudio(now time.Time, lease time.Duration) (*AudioJob, error) {
	return s.ClaimAudioSource(now, lease, "")
}
func (s *Store) ClaimAudioSource(now time.Time, lease time.Duration, source string) (*AudioJob, error) {
	var job *AudioJob
	err := s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("UPDATE audio_jobs SET state=CASE WHEN attempts>=3 THEN 'failed' ELSE 'pending' END,token='',lease_until=0,error='processamento interrompido',updated=? WHERE state='processing' AND lease_until<=?", now.Unix(), now.Unix()); e != nil {
			return e
		}
		// Stop processing if tracking has been disabled. Retain pending metadata.
		if source != "" {
			var e error
			source, e = resolveSource(tx, source)
			if e != nil {
				return e
			}
		}
		var j AudioJob
		e := tx.QueryRow("SELECT a.source_id,a.chat,a.metadata,a.attempts FROM audio_jobs a JOIN chats c ON c.id=a.chat WHERE a.state='pending' AND a.next_attempt<=? AND c.tracked=1 AND (?='' OR a.source_id=?) ORDER BY a.updated,a.source_id LIMIT 1", now.Unix(), source, source).Scan(&j.Source, &j.Chat, &j.Metadata, &j.Attempts)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		j.State = "processing"
		j.Attempts++
		j.Token = newSourceID()
		_, e = tx.Exec("UPDATE audio_jobs SET state='processing',attempts=?,token=?,lease_until=?,updated=? WHERE source_id=?", j.Attempts, j.Token, now.Add(lease).Unix(), now.Unix(), j.Source)
		if e == nil {
			job = &j
		}
		return e
	})
	return job, err
}
func (s *Store) ReleaseAudio(j AudioJob, detail string, cancelled bool) error {
	return s.transaction(func(tx *sql.Tx) error {
		source, e := resolveSource(tx, j.Source)
		if e != nil {
			return e
		}
		state := "pending"
		attempts := j.Attempts
		nextAttempt := time.Now().Add(time.Duration(1<<uint(j.Attempts)) * time.Minute).Unix()
		if cancelled {
			attempts--
			detail = "processamento interrompido"
			nextAttempt = 0
		} else if attempts >= 3 {
			state = "failed"
		}
		// Caller supplies categorical errors, never engine output or message content.
		if len(detail) > 256 {
			return errors.New("erro de fila excede limite")
		}
		_, e = tx.Exec("UPDATE audio_jobs SET state=?,attempts=?,error=?,token='',lease_until=0,next_attempt=?,updated=? WHERE source_id=? AND state='processing' AND token=?", state, attempts, detail, nextAttempt, time.Now().Unix(), source, j.Token)
		return e
	})
}
func (s *Store) RetryAudio(source string) error {
	return s.transaction(func(tx *sql.Tx) error {
		source, e := resolveSource(tx, source)
		if e != nil {
			return e
		}
		r, e := tx.Exec("UPDATE audio_jobs SET state='pending',attempts=0,error='',next_attempt=0,updated=? WHERE source_id=? AND (state='failed' OR (state='pending' AND error<>'')) AND metadata IS NOT NULL", time.Now().Unix(), source)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return invalidInput("somente áudios com erro e metadados disponíveis podem ser reenfileirados")
		}
		return nil
	})
}

// Keep a source retryable when the requested operation fails before reserving it.
func (s *Store) NoteAudioRetryInterrupted(source string) error {
	return s.transaction(func(tx *sql.Tx) error {
		id, err := resolveSource(tx, source)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE audio_jobs SET error='tentativa interrompida antes do processamento; confira conexão e ajustes',updated=? WHERE source_id=? AND state='pending' AND error='' AND metadata IS NOT NULL", time.Now().Unix(), id)
		return err
	})
}
func (s *Store) CompleteAudio(j AudioJob, t Transcript) error {
	now := time.Now().UTC()
	t.ObservedAt = &now
	t.Origin = "processing"
	if err := validateTranscript(t); err != nil {
		return err
	}
	return s.transaction(func(tx *sql.Tx) error {
		source, e := resolveSource(tx, j.Source)
		if e != nil {
			return e
		}
		var token, state string
		if e = tx.QueryRow("SELECT token,state FROM audio_jobs WHERE source_id=?", source).Scan(&token, &state); e != nil {
			return e
		}
		if token != j.Token || state != "processing" {
			return errors.New("reserva de áudio não está ativa")
		}
		var raw []byte
		if e = tx.QueryRow("SELECT data FROM messages WHERE source_id=?", source).Scan(&raw); e != nil {
			return e
		}
		var m Message
		if e = json.Unmarshal(raw, &m); e != nil {
			return e
		}
		if m.Deleted || m.Kind != "áudio" {
			return errors.New("áudio excluído ou substituído")
		}
		m.Transcript = &t
		encoded, e := json.Marshal(m)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE messages SET data=? WHERE source_id=?", encoded, source); e != nil {
			return e
		}
		if e = recordChange(tx, source, m, encoded); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=?", m.Chat); e != nil {
			return e
		}
		_, e = tx.Exec("UPDATE audio_jobs SET state='done',metadata=NULL,error='',token='',lease_until=0,updated=? WHERE source_id=?", time.Now().Unix(), source)
		return e
	})
}

func validateTranscript(t Transcript) error {
	if strings.TrimSpace(t.Text) == "" || !utf8.ValidString(t.Text) || len(t.Text) > 4*1024*1024 {
		return errors.New("transcrição vazia ou inválida")
	}
	for _, segment := range t.Segments {
		if segment.StartMS < 0 || segment.EndMS < segment.StartMS || !utf8.ValidString(segment.Text) {
			return errors.New("segmento inválido")
		}
	}
	return nil
}

// CorrectTranscript appends a supplied correction without retaining or redownloading media.
func (s *Store) CorrectTranscript(source string, t Transcript) error {
	if err := validateTranscript(t); err != nil {
		return err
	}
	return s.transaction(func(tx *sql.Tx) error {
		id, err := resolveSource(tx, source)
		if err != nil {
			return err
		}
		var raw []byte
		if err = tx.QueryRow("SELECT data FROM messages WHERE source_id=?", id).Scan(&raw); err != nil {
			return err
		}
		var m Message
		if err = json.Unmarshal(raw, &m); err != nil {
			return err
		}
		if m.Deleted || m.Kind != "áudio" || m.Transcript == nil {
			return invalidInput("correção exige áudio com transcrição existente")
		}
		old := *m.Transcript
		old.ObservedAt = nil
		old.Origin = ""
		candidate := t
		candidate.ObservedAt = nil
		candidate.Origin = ""
		if reflect.DeepEqual(old, candidate) {
			return nil
		}
		now := time.Now().UTC()
		t.ObservedAt = &now
		t.Origin = "correction"
		m.Transcript = &t
		encoded, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE messages SET data=? WHERE source_id=?", encoded, id); err != nil {
			return err
		}
		if err = recordChange(tx, id, m, encoded); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=?", m.Chat)
		return err
	})
}
