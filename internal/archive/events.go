package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Event is the framework-independent, immutable public record.
type Event struct {
	SchemaVersion string          `json:"schema_version"`
	EventID       string          `json:"event_id"`
	Sequence      int64           `json:"sequence"`
	EventType     string          `json:"event_type"`
	Source        string          `json:"source"`
	AccountID     string          `json:"account_id"`
	GroupID       string          `json:"group_id"`
	MessageID     string          `json:"message_id"`
	OccurredAt    *time.Time      `json:"occurred_at"`
	ObservedAt    *time.Time      `json:"observed_at"`
	Provenance    EventProvenance `json:"provenance"`
	Payload       map[string]any  `json:"payload"`
}
type EventProvenance struct {
	Origin      string   `json:"origin"`
	Derived     bool     `json:"derived"`
	Limitations []string `json:"limitations"`
}

func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func eventTime(t time.Time) *time.Time {
	if t.IsZero() || t.Equal(time.Unix(0, 0)) {
		return nil
	}
	v := t.UTC()
	return &v
}
func eventAccount(tx *sql.Tx) (string, error) {
	var account string
	err := tx.QueryRow("SELECT value FROM meta WHERE key='account'").Scan(&account)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return account, err
}

func (s *Store) migrateEvents() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,event_id TEXT NOT NULL UNIQUE,chat TEXT NOT NULL,data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS events_chat_seq ON events(chat,seq);
CREATE TABLE IF NOT EXISTS event_state(chat TEXT PRIMARY KEY,cursor INTEGER NOT NULL DEFAULT 0,pending TEXT,next_batch INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS event_batches(chat TEXT NOT NULL,id TEXT NOT NULL,state TEXT NOT NULL,to_cursor INTEGER NOT NULL,entry BLOB NOT NULL,manifest BLOB NOT NULL,PRIMARY KEY(chat,id));
CREATE TABLE IF NOT EXISTS reaction_state(key TEXT PRIMARY KEY,emoji TEXT,occurred TEXT);
CREATE TABLE IF NOT EXISTS event_migration(source_id TEXT PRIMARY KEY,revision INTEGER NOT NULL);
UPDATE chats SET file=substr(file,1,length(file)-3)||'.jsonl',exported=-1 WHERE file LIKE 'chat-%.md';
PRAGMA user_version=7;`)
		if err != nil {
			return err
		}
		account, err := eventAccount(tx)
		if err != nil || account == "" {
			return err
		}
		rows, err := tx.Query("SELECT seq,source_id,revision,operation,data FROM changes WHERE revision>COALESCE((SELECT revision FROM event_migration WHERE source_id=changes.source_id),0) ORDER BY seq")
		if err != nil {
			return err
		}
		changes, err := scanChanges(rows)
		if err != nil {
			return err
		}
		for _, c := range changes {
			var prior []byte
			if c.Revision > 1 {
				if err = tx.QueryRow("SELECT data FROM changes WHERE source_id=? AND revision=?", c.Source, c.Revision-1).Scan(&prior); err != nil {
					return err
				}
			}
			// Legacy observations have no recoverable observation timestamp. Never use migration time.
			if err = recordMessageEvents(tx, c.Source, c.Message, prior, c.Revision); err != nil {
				return err
			}
			if _, err = tx.Exec("INSERT INTO event_migration VALUES(?,?) ON CONFLICT(source_id) DO UPDATE SET revision=excluded.revision", c.Source, c.Revision); err != nil {
				return err
			}
		}
		return nil
	})
}
func newEvent(tx *sql.Tx, m Message, kind string) (Event, error) {
	account, err := eventAccount(tx)
	e := Event{SchemaVersion: "1.0", EventType: kind, Source: "whatsapp", AccountID: account, GroupID: m.Chat, MessageID: m.ID, OccurredAt: eventTime(m.Time), Provenance: EventProvenance{Origin: "migration", Limitations: []string{}}, Payload: map[string]any{}}
	if m.Evidence != nil {
		e.ObservedAt = m.Evidence.ObservedAt
		e.Provenance.Origin = m.Evidence.Origin
	}
	if e.ObservedAt == nil {
		e.Provenance.Limitations = append(e.Provenance.Limitations, "observation_time_unavailable")
	}
	if e.OccurredAt == nil {
		e.Provenance.Limitations = append(e.Provenance.Limitations, "source_time_unavailable")
	}
	return e, err
}

// storeEvent excludes observation-time and contextual snapshots from occurrence identity.
func storeEvent(tx *sql.Tx, e Event, identity any) error {
	if e.AccountID == "" {
		return nil
	}
	key, err := json.Marshal([]any{e.AccountID, e.GroupID, identity})
	if err != nil {
		return err
	}
	e.EventID = "evt_" + digest(key)
	result, err := tx.Exec("INSERT OR IGNORE INTO events(event_id,chat,data) VALUES(?,?,?)", e.EventID, e.GroupID, []byte("{}"))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	e.Sequence, err = result.LastInsertId()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE events SET data=? WHERE event_id=?", raw, e.EventID)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE chats SET generation=generation+1 WHERE id=?", e.GroupID)
	return err
}
func messageKind(m Message) string {
	switch m.Kind {
	case "texto":
		return "text"
	case "áudio":
		return "audio"
	case "imagem":
		return "image"
	case "vídeo":
		return "video"
	case "documento":
		return "document"
	}
	return "other"
}
func nativeKey(m Message) map[string]any {
	sender := m.Sender
	if m.Evidence != nil && m.Evidence.NativeSender != "" {
		sender = m.Evidence.NativeSender
	}
	return map[string]any{"message_id": m.ID, "sender_id": sender, "from_me": m.FromMe}
}
func messagePayload(tx *sql.Tx, source string, m Message) (map[string]any, error) {
	var version int
	if err := tx.QueryRow("SELECT COUNT(*) FROM changes WHERE source_id=?", source).Scan(&version); err != nil {
		return nil, err
	}
	text := nullableText(m.Text)
	if messageKind(m) == "text" && !m.Deleted {
		text = &m.Text
	}
	var caption *string
	if messageKind(m) == "image" || messageKind(m) == "video" || messageKind(m) == "document" {
		caption = text
		text = nil
	}
	var groupName *string
	mentions := []string(nil)
	attachments := []Attachment(nil)
	if m.FileName != "" {
		attachments = []Attachment{{ID: m.ID + ":0", Name: nullableText(m.FileName), Availability: "not_retained"}}
	}
	aliases := []string(nil)
	var quoted *string
	var forwarded *bool
	var score *uint32
	nativeSender := m.Sender
	if o := m.Evidence; o != nil {
		caption = o.Caption
		groupName = nullableText(o.GroupName)
		mentions = o.Mentions
		attachments = o.Attachments
		aliases = o.SenderAliases
		quoted = o.QuotedText
		forwarded = o.Forwarded
		score = o.ForwardingScore
		nativeSender = o.NativeSender
	}
	status := "available"
	if m.Deleted {
		status = "deleted"
	} else if m.Kind == "indisponível" || strings.HasPrefix(m.Kind, "não interpretado") {
		status = "unavailable"
	}
	return map[string]any{"source_id": source, "content_version": version + 1, "native_key": nativeKey(m), "sender": map[string]any{"participant_id": m.Sender, "native_id": nativeSender, "aliases": aliases, "display_name": nullableText(m.Author)}, "group": map[string]any{"group_id": m.Chat, "display_name": groupName}, "sent_at": eventTime(m.Time), "message_type": messageKind(m), "original_type": m.Kind, "text": text, "caption": caption, "reply": map[string]any{"message_id": nullableText(m.ReplyID), "sender_id": nullableText(m.ReplySender), "quoted_text": quoted}, "mentions": mentions, "forwarded": forwarded, "forwarding_score": score, "attachments": attachments, "reactions_availability": reactionAvailability(m.Evidence), "edited": m.Edited, "deleted": m.Deleted, "content_status": status}, nil
}
func recordMessageEvents(tx *sql.Tx, source string, m Message, previous []byte, revision int) error {
	// Older schema migrations call recordChange before this additive schema exists.
	var exists int
	if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='events'").Scan(&exists); err != nil || exists == 0 {
		return err
	}
	account, err := eventAccount(tx)
	if err != nil || account == "" {
		return err
	}
	if legacyReaction(m) {
		m.Kind = "reação"
		m.Text = "Reação indisponível"
	}
	if m.Kind == "reação" {
		return recordReaction(tx, m)
	}
	var old Message
	if len(previous) > 0 {
		if err = json.Unmarshal(previous, &old); err != nil {
			return err
		}
	}
	kind := "message.created"
	if len(previous) == 0 && m.Mutation {
		if m.Deleted {
			kind = "message.deleted"
		} else if m.Edited {
			kind = "message.edited"
		}
	}
	if len(previous) > 0 {
		switch {
		case m.Deleted && !old.Deleted:
			kind = "message.deleted"
		case m.Edited && (!old.Edited || m.Version > old.Version || m.Text != old.Text):
			kind = "message.edited"
		default:
			kind = "message.enriched"
		}
	}
	// A transcript-only change is not an edit or a duplicate creation.
	plainOld, plainNew := old, m
	plainOld.Transcript = nil
	plainNew.Transcript = nil
	transcriptChanged := m.Transcript != nil && !reflect.DeepEqual(old.Transcript, m.Transcript)
	if len(previous) == 0 || !reflect.DeepEqual(plainOld, plainNew) {
		e, err := newEvent(tx, m, kind)
		if err != nil {
			return err
		}
		e.Payload, err = messagePayload(tx, source, m)
		if err != nil {
			return err
		}
		e.Payload["content_version"] = revision
		if len(previous) == 0 && (m.Edited || m.Deleted) {
			e.Provenance.Limitations = append(e.Provenance.Limitations, "initial_state_already_mutated")
		}
		if kind == "message.edited" || kind == "message.deleted" {
			e.OccurredAt = nil
			if m.Evidence != nil {
				e.OccurredAt = m.Evidence.EventTime
			}
			e.Payload["sent_at"] = eventTime(old.Time)
			if old.Mutation || len(previous) == 0 {
				e.Payload["sent_at"] = nil
			}
			if m.Version > 0 {
				t := time.UnixMilli(m.Version).UTC()
				e.OccurredAt = &t
			}
		}
		// Fingerprint omits private wire data and observation time. Versions keep repeated-content edits distinct.
		if m.Mutation && len(previous) == 0 {
			e.Payload["sent_at"] = nil
			e.Provenance.Limitations = append(e.Provenance.Limitations, "original_send_time_unavailable")
		}
		identity := []any{source, kind, m.Version, e.Payload}
		if err = storeEvent(tx, e, identity); err != nil {
			return err
		}
	}
	if transcriptChanged {
		e, err := newEvent(tx, m, "message.transcribed")
		if err != nil {
			return err
		}
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM events WHERE json_extract(data,'$.event_type')='message.transcribed' AND json_extract(data,'$.payload.source_id')=?", source).Scan(&count); err != nil {
			return err
		}
		t := m.Transcript
		e.OccurredAt = nil
		e.ObservedAt = t.ObservedAt
		if t.Origin != "" {
			e.Provenance.Origin = t.Origin
		}
		e.Provenance.Limitations = append(e.Provenance.Limitations, "original_audio_not_retained")
		if e.ObservedAt == nil && !containsLimitation(e.Provenance.Limitations, "observation_time_unavailable") {
			e.Provenance.Limitations = append(e.Provenance.Limitations, "observation_time_unavailable")
		}
		segments := make([]map[string]any, 0, len(t.Segments))
		for _, s := range t.Segments {
			segments = append(segments, map[string]any{"start_ms": s.StartMS, "end_ms": s.EndMS, "text": s.Text})
		}
		e.Payload = map[string]any{"source_id": source, "native_key": nativeKey(m), "transcription_version": count + 1, "text": t.Text, "language": nullableText(t.Language), "model": nullableText(t.Model), "model_sha256": nullableText(t.ModelSHA256), "engine": nullableText(t.Engine), "processed_at": eventTime(t.Created), "attachment_id": m.ID + ":0", "file_reference": nil, "original_retained": false, "segments": segments}
		// Processing timestamp distinguishes deliberate reprocessing, including unchanged text.
		if err = storeEvent(tx, e, []any{source, "transcription", t}); err != nil {
			return err
		}
	}
	return nil
}
func targetSnapshot(tx *sql.Tx, m Message, r ReactionObservation) (map[string]any, error) {
	out := map[string]any{"message_id": r.TargetID, "sender_id": nullableText(r.TargetSender), "from_me": r.TargetFromMe, "message_type": nil, "text": nil, "caption": nil, "transcription_text": nil, "transcription_version": nil, "content_version": nil, "source_id": nil, "content_status": "unavailable"}
	if r.TargetID == "" {
		return out, nil
	}
	sender := r.TargetSender
	if sender != "" {
		var err error
		sender, err = canonical(tx, sender)
		if err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query("SELECT data,source_id FROM messages WHERE chat=? AND id=? AND (?='' OR sender=?) AND from_me=?", m.Chat, r.TargetID, sender, sender, r.TargetFromMe)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // Explicit close reports errors before further queries.
	type item struct {
		raw    []byte
		source string
	}
	var found []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.raw, &i.source); err != nil {
			break
		}
		found = append(found, i)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	if len(found) != 1 {
		if len(found) > 1 {
			out["content_status"] = "ambiguous"
		}
		return out, nil
	}
	var target Message
	if err = json.Unmarshal(found[0].raw, &target); err != nil {
		return nil, err
	}
	p, err := messagePayload(tx, found[0].source, target)
	if err != nil {
		return nil, err
	}
	out["source_id"] = found[0].source
	out["sender_id"] = target.Sender
	out["message_type"] = p["message_type"]
	out["text"] = p["text"]
	out["caption"] = p["caption"]
	out["content_status"] = p["content_status"]
	var version int
	if err = tx.QueryRow("SELECT COUNT(*) FROM changes WHERE source_id=?", found[0].source).Scan(&version); err != nil {
		return nil, err
	}
	out["content_version"] = version
	if target.Transcript != nil {
		out["transcription_text"] = target.Transcript.Text
		var n int
		if err = tx.QueryRow("SELECT COUNT(*) FROM events WHERE json_extract(data,'$.event_type')='message.transcribed' AND json_extract(data,'$.payload.source_id')=?", found[0].source).Scan(&n); err != nil {
			return nil, err
		}
		out["transcription_version"] = n
	}
	return out, nil
}
func recordReaction(tx *sql.Tx, m Message) error {
	r := ReactionObservation{EventMessageID: m.ID, ActorID: m.Sender, ActorName: nullableText(m.Author), TargetID: m.ReplyID, TargetSender: m.ReplySender}
	if m.Evidence != nil && m.Evidence.Reaction != nil {
		r = *m.Evidence.Reaction
	} else {
		if m.Text != "Reação indisponível" && m.Text != "" {
			emoji := m.Text
			if emoji == "Reação removida" {
				emoji = ""
			}
			r.Emoji = &emoji
		}
	}
	e, err := newEvent(tx, m, "reaction.added")
	if err != nil {
		return err
	}
	e.MessageID = r.TargetID
	e.OccurredAt = r.Time
	actor, err := canonical(tx, r.ActorID)
	if err != nil {
		return err
	}
	if actor == "" {
		actor = m.Sender
	}
	stateKeyBytes, err := json.Marshal([]any{e.AccountID, m.Chat, r.TargetID, r.TargetSender, r.TargetFromMe, actor})
	if err != nil {
		return err
	}
	stateKey := digest(stateKeyBytes)
	var prior sql.NullString
	var priorTime sql.NullString
	err = tx.QueryRow("SELECT emoji,occurred FROM reaction_state WHERE key=?", stateKey).Scan(&prior, &priorTime)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	known := false
	if r.Time != nil && priorTime.Valid {
		t, err := time.Parse(time.RFC3339Nano, priorTime.String)
		if err != nil {
			return err
		}
		known = r.Time.After(t)
	}
	var previous *string
	if known {
		previous = &prior.String
	}
	if r.Emoji == nil {
		e.EventType = "reaction.snapshot"
		e.Provenance.Limitations = append(e.Provenance.Limitations, "reaction_content_unavailable")
	} else if *r.Emoji == "" {
		e.EventType = "reaction.removed"
	} else if known && prior.Valid && prior.String != "" && prior.String != *r.Emoji {
		e.EventType = "reaction.updated"
		e.Provenance.Derived = true
	}
	target, err := targetSnapshot(tx, m, r)
	if err != nil {
		return err
	}
	if m.Evidence == nil {
		target = map[string]any{"message_id": r.TargetID, "sender_id": nullableText(r.TargetSender), "from_me": r.TargetFromMe, "message_type": nil, "text": nil, "caption": nil, "transcription_text": nil, "transcription_version": nil, "content_version": nil, "source_id": nil, "content_status": "unavailable"}
		e.Provenance.Limitations = append(e.Provenance.Limitations, "historical_target_snapshot_unavailable")
	}
	e.Payload = map[string]any{"actor": map[string]any{"participant_id": actor, "native_id": r.ActorID, "display_name": r.ActorName}, "emoji": r.Emoji, "previous_emoji": previous, "previous_state_known": known, "reaction_message_id": r.EventMessageID, "target_message": target}
	if e.EventType == "reaction.snapshot" {
		e.Payload["availability"] = "unavailable"
		e.Payload["completeness"] = "unknown"
		e.Payload["reactions"] = nil
	}
	if r.Time == nil {
		if m.Evidence != nil && m.Evidence.EventTime != nil {
			e.OccurredAt = m.Evidence.EventTime
			e.Provenance.Limitations = append(e.Provenance.Limitations, "reaction_time_from_envelope")
		} else {
			e.Provenance.Limitations = append(e.Provenance.Limitations, "reaction_time_unavailable")
		}
	}
	if err = storeEvent(tx, e, []any{"reaction", r.EventMessageID, r.ActorID, m.FromMe, r.TargetID, r.TargetSender, r.TargetFromMe, r.Emoji, r.Time}); err != nil {
		return err
	}
	if r.Emoji != nil && r.Time != nil && (!priorTime.Valid || known) {
		_, err = tx.Exec("INSERT INTO reaction_state VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET emoji=excluded.emoji,occurred=excluded.occurred", stateKey, *r.Emoji, r.Time.Format(time.RFC3339Nano))
	}
	return err
}
func recordReactionSnapshots(tx *sql.Tx, m Message) error {
	if m.Evidence == nil || (!m.Evidence.ReactionsAvailable && len(m.Evidence.Reactions) == 0) {
		return nil
	}
	e, err := newEvent(tx, m, "reaction.snapshot")
	if err != nil {
		return err
	}
	e.OccurredAt = nil
	list := make([]map[string]any, 0, len(m.Evidence.Reactions))
	for _, r := range m.Evidence.Reactions {
		if r.ActorIsAccount {
			r.ActorID = e.AccountID
		}
		if r.ActorID == "" {
			e.Provenance.Limitations = append(e.Provenance.Limitations, "reaction_actor_unavailable")
		}
		actor, err := canonical(tx, r.ActorID)
		if err != nil {
			return err
		}
		list = append(list, map[string]any{"actor": map[string]any{"participant_id": actor, "native_id": r.ActorID, "display_name": r.ActorName}, "emoji": r.Emoji, "occurred_at": r.Time, "reaction_message_id": r.EventMessageID})
	}
	r := ReactionObservation{TargetID: m.ID, TargetSender: m.Sender, TargetFromMe: m.FromMe}
	target, err := targetSnapshot(tx, m, r)
	if err != nil {
		return err
	}
	e.Payload = map[string]any{"reactions": list, "availability": "available", "completeness": snapshotCompleteness(m.Evidence), "target_message": target}
	return storeEvent(tx, e, []any{"reaction.snapshot", nativeKey(m), m.Evidence.Reactions, snapshotCompleteness(m.Evidence)})
}

// EventStatus tracks acknowledged JSONL events independently of full-file exports.
type EventStatus struct {
	Cursor    int64   `json:"cursor"`
	Pending   *string `json:"pending"`
	Remaining int     `json:"remaining"`
}

func (s *Store) EventStatus(group string) (EventStatus, error) {
	var status EventStatus
	err := s.transaction(func(tx *sql.Tx) error {
		var pending sql.NullString
		err := tx.QueryRow("SELECT cursor,pending FROM event_state WHERE chat=?", group).Scan(&status.Cursor, &pending)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if pending.Valid {
			status.Pending = &pending.String
		}
		return tx.QueryRow("SELECT COUNT(*) FROM events WHERE chat=? AND seq>?", group, status.Cursor).Scan(&status.Remaining)
	})
	return status, err
}
func requireEventAccount(tx *sql.Tx) error {
	a, err := eventAccount(tx)
	if err != nil {
		return err
	}
	if a == "" {
		return fmt.Errorf("conta não vinculada; execute wpp login antes de exportar eventos")
	}
	return nil
}

func reactionAvailability(o *Observation) string {
	if o != nil && (o.ReactionsAvailable || len(o.Reactions) > 0) {
		return "available"
	}
	return "unavailable"
}
func snapshotCompleteness(o *Observation) string {
	switch o.ReactionsCompleteness {
	case "complete", "partial":
		return o.ReactionsCompleteness
	}
	return "unknown"
}

func containsLimitation(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
