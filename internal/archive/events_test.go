package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func eventFixture(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = s.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err = s.Discover("wedding@g.us", "Casamento Ana e Bia"); err != nil {
		t.Fatal(err)
	}
	if err = s.Track("wedding@g.us", true); err != nil {
		t.Fatal(err)
	}
	return s, dir
}
func evidenceMessage(id, text string) Message {
	now := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	return Message{Chat: "wedding@g.us", ID: id, Sender: "ana@lid", Author: "Ana", Group: true, Time: now, Kind: "texto", Text: text, Evidence: &Observation{ObservedAt: &now, Origin: "live", NativeSender: "ana@lid", Attachments: []Attachment{}, Mentions: []string{}}}
}
func ingestEvent(t *testing.T, s *Store, m Message) {
	t.Helper()
	if _, err := s.Ingest([]Message{m}); err != nil {
		t.Fatal(err)
	}
}
func allEvents(t *testing.T, s *Store) []Event {
	t.Helper()
	rows, err := s.db.Query("SELECT data FROM events ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // Explicit close and Rows.Err are checked below.
	var out []Event
	for rows.Next() {
		var raw []byte
		var e Event
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		validateEventBytes(t, raw)
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}
func reactionMessage(id, target, actor, emoji string, second int) Message {
	m := evidenceMessage(id, emoji)
	m.Kind = "reação"
	m.Sender = actor
	m.Author = "Convidado"
	m.ReplyID = target
	m.ReplySender = "ana@lid"
	when := m.Time.Add(time.Duration(second) * time.Second)
	m.Evidence.NativeSender = actor
	m.Evidence.ObservedAt = &when
	m.Evidence.Reaction = &ReactionObservation{EventMessageID: id, ActorID: actor, ActorName: nullableText(m.Author), TargetID: target, TargetSender: "ana@lid", Emoji: &emoji, Time: &when}
	return m
}
func TestEventsReactionsAndFrozenEvidence(t *testing.T) {
	s, _ := eventFixture(t)
	original := evidenceMessage("text", "Degustação sábado às 10h?\nSem alterações.")
	ingestEvent(t, s, original)
	reply := evidenceMessage("reply", "Pode ser!")
	reply.ReplyID = original.ID
	reply.ReplySender = original.Sender
	reply.Evidence.QuotedText = &original.Text
	ingestEvent(t, s, reply)
	for _, m := range []Message{reactionMessage("r1", "text", "bia@lid", "👍🏽", 1), reactionMessage("r2", "text", "bia@lid", "👩🏽‍❤️‍👩🏻", 2), reactionMessage("r3", "text", "bia@lid", "", 3), reactionMessage("r4", "text", "carla@lid", "✅", 4), reactionMessage("r5", "missing", "dani@lid", "❤️", 5)} {
		ingestEvent(t, s, m)
		ingestEvent(t, s, m)
	}
	edit := original
	edit.Edited = true
	edit.Mutation = true
	edit.Version = original.Time.Add(time.Minute).UnixMilli()
	edit.Text = "Domingo às 11h?"
	edit.Evidence = &Observation{Origin: "live", ObservedAt: eventTime(original.Time.Add(time.Minute)), NativeSender: original.Sender}
	ingestEvent(t, s, edit)
	events := allEvents(t, s)
	kinds := []string{"message.created", "message.created", "reaction.added", "reaction.updated", "reaction.removed", "reaction.added", "reaction.added", "message.edited"}
	if len(events) != len(kinds) {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	for i, e := range events {
		if e.EventType != kinds[i] {
			t.Fatalf("%d: %s", i, e.EventType)
		}
	}
	if events[3].Payload["previous_emoji"] != "👍🏽" || !events[3].Provenance.Derived {
		t.Fatal(events[3])
	}
	if events[2].Payload["target_message"].(map[string]any)["text"] != original.Text {
		t.Fatal("original snapshot lost")
	}
	if events[6].Payload["target_message"].(map[string]any)["content_status"] != "unavailable" {
		t.Fatal(events[6])
	}
	if events[1].Payload["reply"].(map[string]any)["quoted_text"] != original.Text {
		t.Fatal("quoted text lost")
	}
	// Receiving an older occurrence must not regress the current reaction state.
	ingestEvent(t, s, reactionMessage("late", "text", "bia@lid", "👎", 1))
	ingestEvent(t, s, reactionMessage("r6", "text", "bia@lid", "👍", 6))
	last := allEvents(t, s)
	if last[len(last)-1].Payload["previous_emoji"] != "" {
		t.Fatal("late reaction changed latest state")
	}
	ingestEvent(t, s, evidenceMessage("missing", "A mensagem chegou depois."))
	// Historical event remains immutable; native target key is sufficient for the late join.
	if allEvents(t, s)[6].Payload["target_message"].(map[string]any)["text"] != nil {
		t.Fatal("historical snapshot rewritten")
	}
}
func TestEventsAudioTranscriptionVersions(t *testing.T) {
	s, _ := eventFixture(t)
	m := evidenceMessage("audio", "")
	m.Kind = "áudio"
	m.AudioMetadata = []byte("private-download-descriptor")
	ingestEvent(t, s, m)
	for i, text := range []string{"A degustação será sábado.", "A degustação será domingo."} {
		var job *AudioJob
		var err error
		if i == 0 {
			job, err = s.ClaimAudio(time.Now(), time.Minute)
			if err != nil || job == nil {
				t.Fatal(job, err)
			}
		}

		tr := Transcript{Text: text, Language: "pt", Model: "whisper", ModelSHA256: strings.Repeat("a", 64), Engine: "whisper.cpp", Created: time.Now().UTC(), Segments: []TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: text}}}
		if i == 0 {
			err = s.CompleteAudio(*job, tr)
		} else {
			var source string
			err = s.db.QueryRow("SELECT source_id FROM messages WHERE id='audio'").Scan(&source)
			if err == nil {
				err = s.CorrectTranscript(source, tr)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	events := allEvents(t, s)
	if len(events) != 3 || events[1].EventType != "message.transcribed" || events[2].Payload["transcription_version"] != float64(2) {
		t.Fatal(events)
	}
	r := reactionMessage("audio-reaction", "audio", "bia@lid", "👍", 10)
	ingestEvent(t, s, r)
	events = allEvents(t, s)
	target := events[3].Payload["target_message"].(map[string]any)
	if target["transcription_text"] != "A degustação será domingo." || target["transcription_version"] != float64(2) {
		t.Fatal(target)
	}
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private-download")) {
		t.Fatal("download secret leaked")
	}
}
func TestEventsSnapshotsAndUnknownDates(t *testing.T) {
	s, _ := eventFixture(t)
	m := evidenceMessage("snapshot", "Mensagem")
	m.Time = time.Time{}
	m.Evidence.ReactionsAvailable = true
	m.Evidence.ReactionsCompleteness = "complete"
	ingestEvent(t, s, m)
	events := allEvents(t, s)
	if len(events) != 2 || events[0].OccurredAt != nil || len(events[1].Payload["reactions"].([]any)) != 0 {
		t.Fatal(events)
	}
	m.Evidence = &Observation{Origin: "history", ObservedAt: eventTime(time.Now()), Reactions: []ReactionObservation{{ActorID: "bia@lid", Emoji: nullableText("👍")}, {ActorID: "carla@lid", Emoji: nullableText("❤️")}}}
	ingestEvent(t, s, m)
	ingestEvent(t, s, m)
	events = allEvents(t, s)
	if len(events) != 3 || events[2].EventType != "reaction.snapshot" || events[2].Payload["completeness"] != "unknown" {
		t.Fatal(events)
	}
	unknown := reactionMessage("encrypted", "snapshot", "bia@lid", "", 20)
	unknown.Evidence.Reaction.Emoji = nil
	ingestEvent(t, s, unknown)
	events = allEvents(t, s)
	if events[3].Payload["reactions"] != nil || events[3].Payload["availability"] != "unavailable" {
		t.Fatal(events[3])
	}
}
func TestEventsSchemaAndSyntheticExamples(t *testing.T) {
	raw, err := os.ReadFile("../../docs/schemas/events-1.0.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err = c.AddResource("urn:wpp:events:1.0", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("urn:wpp:events:1.0")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := eventFixture(t)
	m := evidenceMessage("schema", "Texto\noriginal")
	ingestEvent(t, s, m)
	ingestEvent(t, s, reactionMessage("r", "schema", "bia@lid", "👍", 1))
	ingestEvent(t, s, reactionMessage("missing-r", "absent", "bia@lid", "❤️", 2))
	for _, e := range allEvents(t, s) {
		raw, err = json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(v); err != nil {
			t.Fatal(err)
		}
	}
	example, err := os.ReadFile("../../docs/examples/events-1.0.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(example), []byte("\n")) {
		var v any
		if err = json.Unmarshal(line, &v); err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(v); err != nil {
			t.Fatal(err)
		}
	}
	invalid := map[string]any{"schema_version": "1.0"}
	if schema.Validate(invalid) == nil {
		t.Fatal("invalid envelope accepted")
	}
}
func TestEventsMigrationStableAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Track("wedding@g.us", true); err != nil {
		t.Fatal(err)
	}
	m := evidenceMessage("legacy", "Texto antigo")
	m.Evidence = nil
	ingestEvent(t, s, m)
	if err = s.BindAccount("account@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateEvents(); err != nil {
		t.Fatal(err)
	}
	before := allEvents(t, s)
	if len(before) != 1 || before[0].ObservedAt != nil {
		t.Fatal(before)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	after := allEvents(t, s)
	if len(after) != 1 || after[0].EventID != before[0].EventID {
		t.Fatal(after)
	}
	b := EventBatchService{Store: s, Dir: filepath.Join(dir, "exports")}
	if _, err = b.Next(m.Chat); err != nil {
		t.Fatal(err)
	}
	if len(allEvents(t, s)) != 1 {
		t.Fatal("backfill duplicated events")
	}
}

func TestEventsLegacyAudioAndReactionDoNotInventObservation(t *testing.T) {
	s, _ := eventFixture(t)
	m := evidenceMessage("legacy-audio", "")
	m.Kind = "áudio"
	m.Evidence = nil
	m.Transcript = &Transcript{Text: "Fala previamente processada", Created: m.Time, Language: "pt"}
	ingestEvent(t, s, m)
	legacy := evidenceMessage("legacy-reaction", "")
	legacy.Evidence = nil
	legacy.Kind = "não interpretado (reactionMessage)"
	ingestEvent(t, s, legacy)
	events := allEvents(t, s)
	if len(events) != 3 || events[1].EventType != "message.transcribed" || events[1].ObservedAt != nil || events[2].EventType != "reaction.snapshot" {
		t.Fatal(events)
	}
	if events[2].Payload["target_message"].(map[string]any)["content_status"] != "unavailable" {
		t.Fatal(events[2])
	}
	// A known legacy reaction can keep its emoji, but cannot reconstruct the target's old content.
	r := reactionMessage("known-old", "legacy-audio", "bia@lid", "👍", 1)
	r.Evidence = nil
	ingestEvent(t, s, r)
	events = allEvents(t, s)
	last := events[len(events)-1]
	if last.ObservedAt != nil || last.Payload["target_message"].(map[string]any)["text"] != nil {
		t.Fatal(last)
	}
}

var eventSchemaOnce sync.Once
var eventSchema *jsonschema.Schema
var eventSchemaError error

func validateEventBytes(t *testing.T, raw []byte) {
	t.Helper()
	eventSchemaOnce.Do(func() {
		var schemaBytes []byte
		schemaBytes, eventSchemaError = os.ReadFile("../../docs/schemas/events-1.0.schema.json")
		if eventSchemaError != nil {
			return
		}
		var doc any
		eventSchemaError = json.Unmarshal(schemaBytes, &doc)
		if eventSchemaError != nil {
			return
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		eventSchemaError = c.AddResource("urn:wpp:events:1.0", doc)
		if eventSchemaError != nil {
			return
		}
		eventSchema, eventSchemaError = c.Compile("urn:wpp:events:1.0")
	})
	if eventSchemaError != nil {
		t.Fatal(eventSchemaError)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if err := eventSchema.Validate(doc); err != nil {
		t.Fatalf("event schema: %v\n%s", err, raw)
	}
}

func TestEventsMutationBeforeOriginalAndAmbiguousTarget(t *testing.T) {
	s, _ := eventFixture(t)
	edit := evidenceMessage("late-original", "Domingo")
	edit.Edited = true
	edit.Mutation = true
	edit.Version = edit.Time.Add(time.Minute).UnixMilli()
	edit.Evidence.EventTime = eventTime(time.UnixMilli(edit.Version))
	ingestEvent(t, s, edit)
	first := allEvents(t, s)
	if len(first) != 1 || first[0].EventType != "message.edited" || first[0].Payload["sent_at"] != nil {
		t.Fatal(first)
	}
	ingestEvent(t, s, evidenceMessage("late-original", "Sábado"))
	events := allEvents(t, s)
	if len(events) != 2 || events[1].EventType != "message.enriched" || events[1].Payload["text"] != "Domingo" || events[1].Payload["sent_at"] == nil {
		t.Fatal(events)
	}
	a := evidenceMessage("collision", "Primeiro autor")
	ingestEvent(t, s, a)
	b := a
	b.Sender = "other@lid"
	b.Author = "Outro autor"
	b.Evidence = nil
	ingestEvent(t, s, b)
	r := reactionMessage("ambiguous", "collision", "bia@lid", "👍", 5)
	r.Evidence.Reaction.TargetSender = ""
	r.ReplySender = ""
	ingestEvent(t, s, r)
	events = allEvents(t, s)
	if events[len(events)-1].Payload["target_message"].(map[string]any)["content_status"] != "ambiguous" {
		t.Fatal("ambiguous target guessed")
	}
	r = reactionMessage("envelope", "not-here", "bia@lid", "👍", 6)
	r.Evidence.EventTime = r.Evidence.Reaction.Time
	r.Evidence.Reaction.Time = nil
	ingestEvent(t, s, r)
	events = allEvents(t, s)
	if events[len(events)-1].OccurredAt == nil {
		t.Fatal("available envelope timestamp lost")
	}
}
