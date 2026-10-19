package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

type BrowserMessage struct {
	SourceID         string        `json:"source_id"`
	Message          Message       `json:"message"`
	Route            string        `json:"route"`
	Author           string        `json:"author"`
	ReplyAuthor      string        `json:"reply_author"`
	Reply            *BrowserReply `json:"reply,omitempty"`
	PreviewTruncated bool          `json:"preview_truncated"`
	AudioState       string        `json:"audio_state,omitempty"`
}
type MessagePage struct {
	Items  []BrowserMessage `json:"items"`
	Offset int              `json:"offset"`
	More   bool             `json:"more"`
}

func (s *Store) BrowseMessages(chat, query, person string, offset, limit int) (MessagePage, error) {
	page := MessagePage{Items: []BrowserMessage{}, Offset: offset}
	if offset < 0 || limit < 1 || limit > 100 || len(query) > 512 {
		return page, invalidInput("filtro ou paginação inválidos")
	}
	names, err := s.Names()
	if err != nil {
		return page, err
	}
	err = s.transaction(func(tx *sql.Tx) error {
		var exists int
		if e := tx.QueryRow("SELECT 1 FROM chats WHERE id=?", chat).Scan(&exists); e != nil {
			if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			return invalidInput("conversa não encontrada")
		}
		if person != "" {
			var e error
			person, e = canonical(tx, person)
			if e != nil {
				return e
			}
		}
		rows, e := tx.Query("SELECT source_id,data FROM messages WHERE chat=? AND (?='' OR json_extract(data,'$.Text') LIKE ? ESCAPE '\\' OR json_extract(data,'$.Transcript.Text') LIKE ? ESCAPE '\\') AND (?='' OR json_extract(data,'$.Sender')=?) ORDER BY ts DESC,id DESC,sender DESC,from_me DESC LIMIT ? OFFSET ?", chat, query, "%"+escapeLike(query)+"%", "%"+escapeLike(query)+"%", person, person, limit+1, offset)
		if e != nil {
			return e
		}
		// Rows are also closed explicitly before reply queries use this transaction.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var item BrowserMessage
			var raw []byte
			if e = rows.Scan(&item.SourceID, &raw); e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &item.Message); e != nil {
				return e
			}
			if len(page.Items) == limit {
				page.More = true
				break
			}
			item.Route = route(item.Message, names)
			item.Author = personLabel(names, item.Message.Sender, item.Message.Author)
			if item.Message.ReplySender != "" {
				item.ReplyAuthor = personLabel(names, item.Message.ReplySender, "")
			}
			item.Message.Wire = nil
			if len(item.Message.Text) > 12000 {
				item.Message.Text = trimUTF8(item.Message.Text, 12000)
				item.PreviewTruncated = true
			}
			if item.Message.Transcript != nil {
				item.Message.Transcript.Segments = nil
				if len(item.Message.Transcript.Text) > 12000 {
					item.Message.Transcript.Text = trimUTF8(item.Message.Transcript.Text, 12000)
					item.PreviewTruncated = true
				}
			}
			page.Items = append(page.Items, item)
		}
		if e = rows.Err(); e != nil {
			return errors.Join(e, rows.Close())
		}
		if e = rows.Close(); e != nil {
			return e
		}
		for i := range page.Items {
			item := &page.Items[i]
			if item.Message.Kind == "áudio" {
				e = tx.QueryRow("SELECT state FROM audio_jobs WHERE source_id=?", item.SourceID).Scan(&item.AudioState)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
			}
			if item.Message.ReplyID == "" {
				continue
			}
			item.Reply, e = browserReply(tx, item.Message, names)
			if e != nil {
				return e
			}
			if item.Reply.State == "available" {
				item.ReplyAuthor = item.Reply.Author
			} else {
				item.ReplyAuthor = ""
			}

		}
		return nil
	})
	return page, err
}
func escapeLike(s string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(s)
}
func trimUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (s *Store) SetPersonName(group, id, name string) error {
	if strings.TrimSpace(name) != "" {
		_, err := s.RegisterPeople(PeopleFile{SchemaVersion: 1, GroupID: group, People: []Person{{ID: id, Name: name}}})
		return err
	}
	if err := validateGroup(group); err != nil {
		return err
	}
	if !validPerson(id) {
		return invalidInput("ID de pessoa inválido")
	}
	return s.transaction(func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM group_people WHERE chat=? AND person IN(?,COALESCE((SELECT canonical FROM aliases WHERE alias=?),?))", group, id, id, id).Scan(&exists); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return invalidInput("pessoa não encontrada neste grupo")
		}
		id, err := canonical(tx, id)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM people_names WHERE id=?", id); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE chats SET generation=generation+1")
		return err
	})
}

var inspectionTables = map[string]string{
	"service_operations": "Operações do painel",
	"audio_jobs":         "Fila de áudio (metadados privados de download omitidos)",
	"chats":              "Conversas e acompanhamento", "messages": "Estado atual das mensagens", "people_names": "Nomes cadastrados", "group_people": "Pessoas conhecidas por grupo",
	"aliases": "Identidades alternativas", "attempts": "Tentativas de recuperação", "changes": "Revisões de mensagens", "source_redirects": "Fontes unificadas",
	"analysis_state": "Cursores de análise", "analysis_batches": "Lotes reservados e confirmados", "retired_exports": "Exports duplicados", "meta": "Metadados da conta",
}

type TableInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Count       int    `json:"count"`
}
type TablePage struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Offset  int      `json:"offset"`
	More    bool     `json:"more"`
}

func (s *Store) Tables() ([]TableInfo, error) {
	out := []TableInfo{}
	err := s.transaction(func(tx *sql.Tx) error {
		for name, description := range inspectionTables {
			var count int
			if e := tx.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&count); e != nil {
				return e
			}
			out = append(out, TableInfo{name, description, count})
		}
		return nil
	})
	return out, err
}
func (s *Store) InspectTable(name string, offset, limit int) (TablePage, error) {
	page := TablePage{Columns: []string{}, Rows: [][]any{}, Offset: offset}
	if _, ok := inspectionTables[name]; !ok {
		return page, invalidInput("tabela não disponível para consulta")
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return page, invalidInput("paginação inválida")
	}
	err := s.transaction(func(tx *sql.Tx) error {
		query := fmt.Sprintf("SELECT * FROM %s ORDER BY rowid LIMIT ? OFFSET ?", name)
		if name == "audio_jobs" {
			query = "SELECT source_id,chat,state,attempts,next_attempt,lease_until,error,updated,phase FROM audio_jobs ORDER BY rowid LIMIT ? OFFSET ?"
		}
		rows, e := tx.Query(query, limit+1, offset)
		if e != nil {
			return e
		}
		// Rows are also closed explicitly before reply queries use this transaction.
		defer func() { _ = rows.Close() }()
		page.Columns, e = rows.Columns()
		if e != nil {
			return e
		}
		for rows.Next() {
			values := make([]any, len(page.Columns))
			targets := make([]any, len(values))
			for i := range values {
				targets[i] = &values[i]
			}
			if e = rows.Scan(targets...); e != nil {
				return e
			}
			if len(page.Rows) == limit {
				page.More = true
				break
			}
			for i, v := range values {
				if blob, ok := v.([]byte); ok {
					v = string(blob)
				}
				if text, ok := v.(string); ok && len(text) > 4000 {
					v = trimUTF8(text, 4000) + "\n[prévia limitada a 4000 bytes]"
				}
				values[i] = v
			}
			page.Rows = append(page.Rows, values)
		}
		return rows.Err()
	})
	return page, err
}
