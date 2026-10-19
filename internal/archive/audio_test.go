package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func audioMessage(id string) Message {
	m := message(id, 1)
	m.Kind = "áudio"
	m.Text = ""
	m.AudioMetadata = []byte("PRIVATE MEDIA KEY")
	return m
}
func TestAudioQueueReplayCompletionAndImmutableBatch(t *testing.T) {
	s, b := testBatches(t)
	m := audioMessage("one")
	ingest(t, s, m, m)
	jobs, err := s.AudioJobs(0, 50)
	must(t, err)
	if len(jobs) != 1 || jobs[0].State != "pending" {
		t.Fatal(jobs)
	}
	raw, err := json.Marshal(jobs)
	must(t, err)
	if bytes.Contains(raw, m.AudioMetadata) {
		t.Fatal("private metadata exposed")
	}
	if len(changesFor(t, s)) != 1 {
		t.Fatal("metadata produced message revisions")
	}
	path, manifest, original := nextLot(t, b)
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	if job == nil {
		t.Fatal("no job")
	}
	other, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	if other != nil {
		t.Fatal("duplicate reservation")
	}
	text := "A entrega será feita na sexta-feira. Ação aprovada ✅"
	must(t, s.CompleteAudio(*job, Transcript{Text: text, Language: "pt", Model: "large-v3", Segments: []TranscriptSegment{{StartMS: 0, EndMS: 2500, Text: text}}, Created: time.Now().UTC()}))
	if err = s.CompleteAudio(*job, Transcript{Text: "duplicate"}); err == nil {
		t.Fatal("stale reservation accepted")
	}
	ingest(t, s, m)
	if len(changesFor(t, s)) != 2 {
		t.Fatal("replay removed transcript")
	}
	jobs, err = s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].State != "done" {
		t.Fatal(jobs)
	}
	var metadata []byte
	must(t, s.db.QueryRow("SELECT metadata FROM audio_jobs").Scan(&metadata))
	if metadata != nil {
		t.Fatal("download keys retained after completion")
	}
	same, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Equal(original, same) {
		t.Fatal("published batch changed")
	}
	must(t, b.Ack(m.Chat, manifest.BatchID))
	_, _, entry := nextLot(t, b)
	if !bytes.Contains(entry, []byte(text)) {
		t.Fatal("transcript missing from next batch")
	}
	page, err := s.BrowseMessages(m.Chat, "Ação", "", 0, 50)
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].Message.Text != "" || page.Items[0].Message.Transcript.Text != text {
		t.Fatal(page)
	}
	table, err := s.InspectTable("audio_jobs", 0, 50)
	must(t, err)
	for _, c := range table.Columns {
		if c == "metadata" || c == "token" {
			t.Fatal("private queue column exposed")
		}
	}
}
func TestAudioFailureLeaseCancellationAndRetry(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	m := audioMessage("one")
	ingest(t, s, m)
	now := time.Now()
	first, err := s.ClaimAudio(now, time.Second)
	must(t, err)
	recovered, err := s.ClaimAudio(now.Add(2*time.Second), time.Minute)
	must(t, err)
	if recovered == nil || recovered.Token == first.Token || recovered.Attempts != 2 {
		t.Fatal("lease not recovered", recovered)
	}
	if err = s.CompleteAudio(*first, Transcript{Text: "stale"}); err == nil {
		t.Fatal("old lease completed")
	}
	must(t, s.ReleaseAudio(*recovered, "falha de teste", false))
	third, err := s.ClaimAudio(now.Add(time.Hour), time.Minute)
	must(t, err)
	must(t, s.ReleaseAudio(*third, "falha de teste", false))
	jobs, err := s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].State != "failed" {
		t.Fatal(jobs)
	}
	must(t, s.RetryAudio(first.Source))
	current, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	must(t, s.ReleaseAudio(*current, "ignored", true))
	jobs, err = s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].Attempts != 0 {
		t.Fatal("cancellation consumes attempt")
	}
	current, err = s.ClaimAudio(now.Add(time.Hour), time.Minute)
	must(t, err)
	deleted := m
	deleted.Deleted = true
	deleted.Mutation = true
	deleted.Version = 2000
	ingest(t, s, deleted)
	if err = s.CompleteAudio(*current, Transcript{Text: "must not resurrect"}); err == nil {
		t.Fatal("deleted audio completed")
	}
	jobs, err = s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].State != "cancelled" {
		t.Fatal(jobs)
	}
}
func TestAudioMigrationLateMetadataRollbackAndAlias(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.Track("group@g.us", true))
	m := audioMessage("legacy")
	m.AudioMetadata = nil
	ingest(t, s, m)
	before := changesFor(t, s)
	_, err = s.db.Exec("DROP TABLE audio_jobs; PRAGMA user_version=3")
	must(t, err)
	must(t, s.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	jobs, err := s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].State != "waiting_metadata" {
		t.Fatal(jobs)
	}
	m.AudioMetadata = []byte("metadata")
	ingest(t, s, m)
	jobs, err = s.AudioJobs(0, 50)
	must(t, err)
	if jobs[0].State != "pending" || jobs[0].Source != before[0].Source || len(changesFor(t, s)) != 1 {
		t.Fatal("late metadata changed evidence")
	}
	_, err = s.Ingest([]Message{audioMessage("rollback"), message("", 2)})
	if err == nil {
		t.Fatal("invalid transaction")
	}
	jobs, err = s.AudioJobs(0, 50)
	must(t, err)
	if len(jobs) != 1 {
		t.Fatal("queue escaped rollback")
	}
	must(t, s.Alias(m.Sender, "5511@s.whatsapp.net"))
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	if job.Source != before[0].Source {
		t.Fatal("unstable audio source")
	}
	must(t, s.CompleteAudio(*job, Transcript{Text: "Confirmado", Language: "pt"}))
}
func TestCompletedAudioSurvivesDuplicateIdentityMerge(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	lid := audioMessage("same")
	lid.Sender = "123@lid"
	ingest(t, s, lid)
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	must(t, s.CompleteAudio(*job, Transcript{Text: "Conclusão preservada", Language: "pt"}))
	phone := lid
	phone.Sender = "5511@s.whatsapp.net"
	ingest(t, s, phone)
	must(t, s.Alias(lid.Sender, phone.Sender))
	page, err := s.BrowseMessages(lid.Chat, "", "", 0, 50)
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].Message.Transcript == nil || page.Items[0].Message.Transcript.Text != "Conclusão preservada" {
		t.Fatal("transcript lost during reconciliation", page)
	}
	jobs, err := s.AudioJobs(0, 50)
	must(t, err)
	if len(jobs) != 1 || jobs[0].State != "done" {
		t.Fatal("duplicate work retained", jobs)
	}
}
func TestAudioCompletionRollsBackOnCommitFailure(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, audioMessage("one"))
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	_, err = s.db.Exec("CREATE TRIGGER fail_audio BEFORE UPDATE OF state ON audio_jobs WHEN NEW.state='done' BEGIN SELECT RAISE(ABORT,'simulated failure'); END")
	must(t, err)
	if err = s.CompleteAudio(*job, Transcript{Text: "Confirmado", Language: "pt"}); err == nil {
		t.Fatal("failure not injected")
	}
	page, err := s.BrowseMessages("group@g.us", "", "", 0, 50)
	must(t, err)
	if page.Items[0].Message.Transcript != nil || len(changesFor(t, s)) != 1 {
		t.Fatal("partial completion escaped transaction")
	}
	_, err = s.db.Exec("DROP TRIGGER fail_audio")
	must(t, err)
	must(t, s.CompleteAudio(*job, Transcript{Text: "Confirmado", Language: "pt"}))
}
