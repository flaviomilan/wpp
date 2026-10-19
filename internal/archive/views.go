package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type BrowserReply struct {
	State     string `json:"state"`
	Source    string `json:"source_id,omitempty"`
	Author    string `json:"author,omitempty"`
	Preview   string `json:"preview,omitempty"`
	Edited    bool   `json:"edited,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}
type BrowserAudio struct {
	AudioJob
	Author        string    `json:"author"`
	Time          time.Time `json:"time"`
	Preview       string    `json:"transcript_preview,omitempty"`
	Truncated     bool      `json:"preview_truncated,omitempty"`
	HasTranscript bool      `json:"has_transcript"`
	RetryEligible bool      `json:"retry_eligible"`
	RetryReason   string    `json:"retry_reason,omitempty"`
	Tracked       bool      `json:"tracked"`
	Message       *Message  `json:"message,omitempty"`
}

func messagePreview(m Message) string {
	if m.Deleted {
		return "Mensagem excluída"
	}
	if m.Kind == "áudio" && m.Transcript != nil {
		return "Áudio · " + m.Transcript.Text
	}
	text := m.Text
	if m.Kind != "texto" {
		text = "[" + m.Kind + "] " + text
	}
	if m.FileName != "" {
		text += " · " + m.FileName
	}
	if text == "" {
		text = "[" + m.Kind + "]"
	}
	return text
}
func browserReply(tx *sql.Tx, m Message, names map[string]string) (*BrowserReply, error) {
	reply := &BrowserReply{State: "missing"}
	sender, err := canonical(tx, m.ReplySender)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query("SELECT source_id,data FROM messages WHERE chat=? AND id=? AND (?='' OR sender=?) LIMIT 2", m.Chat, m.ReplyID, sender, sender)
	if err != nil {
		return nil, err
	}
	// Rows.Err reports iteration and automatic-close failures.
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var raw []byte
		var parent Message
		if err = rows.Scan(&reply.Source, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &parent); err != nil {
			return nil, err
		}
		count++
		reply.Author = personLabel(names, parent.Sender, parent.Author)
		if parent.FromMe {
			reply.Author = "Você"
		}
		text := messagePreview(parent)
		reply.Preview = trimUTF8(text, 600)
		reply.Truncated = len(text) > 600
		reply.Edited = parent.Edited
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if count == 1 {
		reply.State = "available"
		return reply, nil
	}
	if count > 1 {
		reply.State = "ambiguous"
	}
	// Never attribute ambiguous references to whichever candidate happened to be read last.
	return &BrowserReply{State: reply.State}, nil
}

func publicMessage(raw []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(raw, &m)
	m.Wire = nil
	m.AudioMetadata = nil
	if m.Transcript != nil {
		m.Transcript.Segments = nil
	}
	return m, err
}
func (s *Store) BrowserSource(source string) (BrowserMessage, error) {
	var item BrowserMessage
	names, err := s.Names()
	if err != nil {
		return item, err
	}
	err = s.transaction(func(tx *sql.Tx) error {
		id, e := resolveSource(tx, source)
		if e != nil {
			return e
		}
		var raw []byte
		if e = tx.QueryRow("SELECT data FROM messages WHERE source_id=?", id).Scan(&raw); e != nil {
			return e
		}
		m, e := publicMessage(raw)
		if e != nil {
			return e
		}
		item = BrowserMessage{SourceID: id, Message: m, Author: personLabel(names, m.Sender, m.Author), Route: route(m, names)}
		if m.FromMe {
			item.Author = "Você"
		}
		return nil
	})
	return item, err
}
func audioView(tx *sql.Tx, job AudioJob, names map[string]string, full bool) (BrowserAudio, error) {
	var raw []byte
	var metadata, tracked bool
	err := tx.QueryRow("SELECT m.data,c.tracked,(a.metadata IS NOT NULL AND length(a.metadata)>0) FROM audio_jobs a JOIN messages m ON m.source_id=a.source_id JOIN chats c ON c.id=a.chat WHERE a.source_id=?", job.Source).Scan(&raw, &tracked, &metadata)
	if err != nil {
		return BrowserAudio{}, err
	}
	return renderAudioView(job, raw, names, tracked, metadata, full)
}
func renderAudioView(job AudioJob, raw []byte, names map[string]string, tracked, metadata, full bool) (BrowserAudio, error) {
	item := BrowserAudio{AudioJob: job, Tracked: tracked}
	m, err := publicMessage(raw)
	if err != nil {
		return item, err
	}
	item.Author = personLabel(names, m.Sender, m.Author)
	if m.FromMe {
		item.Author = "Você"
	}
	item.Time = m.Time
	item.HasTranscript = !m.Deleted && m.Transcript != nil
	if item.HasTranscript {
		item.Preview = trimUTF8(m.Transcript.Text, 600)
		item.Truncated = len(m.Transcript.Text) > 600
	}
	item.RetryEligible = metadata && !m.Deleted && (job.State == "failed" || (job.State == "pending" && job.Error != ""))
	switch {
	case job.State == "done":
		item.RetryReason = "Transcrição concluída."
	case job.State == "processing":
		item.RetryReason = "O áudio já está em processamento."
	case m.Deleted || job.State == "cancelled":
		item.RetryReason = "Mensagem excluída ou substituída."
	case !metadata:
		item.RetryReason = "Sincronize a conversa para tentar recuperar os metadados do áudio."
	case !item.RetryEligible:
		item.RetryReason = "O áudio está aguardando processamento."
	}
	if item.RetryEligible && !item.Tracked {
		item.RetryReason = "Acompanhe a conversa antes de processar este áudio."
	}
	if full {
		item.Message = &m
	}
	return item, nil
}
func (s *Store) BrowseAudioPage(offset, limit int, chat, state string) ([]BrowserAudio, map[string]int, error) {
	items := []BrowserAudio{}
	summary := map[string]int{}
	if offset < 0 || limit < 1 || limit > 101 {
		return nil, nil, errors.New("filtro ou paginação inválidos")
	}
	err := s.transaction(func(tx *sql.Tx) error {
		names, err := namesTx(tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query("SELECT a.source_id,a.chat,a.state,a.attempts,a.error,a.next_attempt,a.phase,m.data,c.tracked,(a.metadata IS NOT NULL AND length(a.metadata)>0) FROM audio_jobs a JOIN messages m ON m.source_id=a.source_id JOIN chats c ON c.id=a.chat WHERE (?='' OR a.chat=?) AND (?='' OR a.state=?) ORDER BY a.updated DESC,a.source_id LIMIT ? OFFSET ?", chat, chat, state, state, limit, offset)
		if err != nil {
			return err
		}
		for rows.Next() {
			var job AudioJob
			var raw []byte
			var tracked, metadata bool
			if err = rows.Scan(&job.Source, &job.Chat, &job.State, &job.Attempts, &job.Error, &job.NextAttempt, &job.Phase, &raw, &tracked, &metadata); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			item, err := renderAudioView(job, raw, names, tracked, metadata, false)
			if err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			items = append(items, item)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		rows, err = tx.Query("SELECT state,COUNT(*) FROM audio_jobs WHERE (?='' OR chat=?) GROUP BY state", chat, chat)
		if err != nil {
			return err
		}
		// Rows.Err reports iteration and automatic-close failures.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var key string
			var count int
			if err = rows.Scan(&key, &count); err != nil {
				return err
			}
			summary[key] = count
		}
		return rows.Err()
	})
	return items, summary, err
}
func readAudioJob(tx *sql.Tx, source string) (AudioJob, error) {
	j := AudioJob{Source: source}
	err := tx.QueryRow("SELECT chat,state,attempts,error,next_attempt,phase FROM audio_jobs WHERE source_id=?", source).Scan(&j.Chat, &j.State, &j.Attempts, &j.Error, &j.NextAttempt, &j.Phase)
	return j, err
}
func (s *Store) BrowserAudioSource(source string) (BrowserAudio, error) {
	var item BrowserAudio
	err := s.transaction(func(tx *sql.Tx) error {
		names, e := namesTx(tx)
		if e != nil {
			return e
		}
		id, e := resolveSource(tx, source)
		if e != nil {
			return e
		}
		job, e := readAudioJob(tx, id)
		if e != nil {
			return e
		}
		item, e = audioView(tx, job, names, true)
		return e
	})
	return item, err
}
