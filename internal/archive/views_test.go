package archive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestReplyContentOutsidePageAndAmbiguity(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	original := message("original", 1)
	original.Author = "Nome recebido"
	original.Text = "Contrato revisado ✅"
	original.Edited = true
	reply := message("reply", 2)
	reply.Sender = "other@lid"
	reply.ReplyID = original.ID
	reply.Text = "reply"
	ingest(t, s, original, reply)
	must(t, s.SetPersonName(original.Chat, original.Sender, "Noiva"))
	page, err := s.BrowseMessages(original.Chat, "", "", 0, 1)
	must(t, err)
	quote := page.Items[0].Reply
	if quote == nil || quote.State != "available" || quote.Author != "Noiva" || quote.Preview != "Contrato revisado ✅" || !quote.Edited {
		t.Fatal(quote)
	}
	detail, err := s.BrowserSource(quote.Source)
	must(t, err)
	if detail.Message.Text != original.Text || detail.Author != "Noiva" || len(detail.Message.Wire) > 0 {
		t.Fatal(detail)
	}
	// Reconcile the sender without losing the source used by the browser.
	must(t, s.Alias(original.Sender, "551199999@s.whatsapp.net"))
	page, err = s.BrowseMessages(original.Chat, "", "", 0, 1)
	must(t, err)
	if page.Items[0].Reply.Author != "Noiva" {
		t.Fatal(page.Items[0].Reply)
	}
	another := original
	another.Sender = "third@lid"
	another.Text = "Different original"
	ingest(t, s, another)
	page, err = s.BrowseMessages(original.Chat, "reply", "", 0, 50)
	must(t, err)
	if page.Items[0].Reply.State != "ambiguous" || page.Items[0].Reply.Author != "" || page.Items[0].Reply.Preview != "" {
		t.Fatal("ambiguous attribution", page.Items[0].Reply)
	}
	reply.ReplyID = "absent"
	reply.Text = "reply missing"
	reply.Edited = true
	reply.Version = 1000
	ingest(t, s, reply)
	page, err = s.BrowseMessages(original.Chat, "reply missing", "", 0, 50)
	must(t, err)
	if page.Items[0].Reply.State != "missing" {
		t.Fatal(page.Items[0].Reply)
	}
}

func TestAudioViewsBoundedPublicAndFullTranscript(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	m := audioMessage("audio")
	m.Wire = []byte("PRIVATE WIRE")
	m.Author = "Mariana"
	ingest(t, s, m)
	job, err := s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	text := strings.Repeat("Entrega confirmada 🗓️\n", 1000)
	must(t, s.CompleteAudio(*job, Transcript{Text: text, Model: "test", Language: "pt"}))
	rows, _, err := s.BrowseAudioPage(0, 50, "group@g.us", "done")
	must(t, err)
	if len(rows) != 1 || !rows[0].HasTranscript || !rows[0].Truncated || len(rows[0].Preview) > 600 || !utf8.ValidString(rows[0].Preview) || rows[0].RetryEligible {
		t.Fatal(rows)
	}
	raw, err := json.Marshal(rows)
	must(t, err)
	if strings.Contains(string(raw), "metadata") || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "token") {
		t.Fatal("private metadata exposed")
	}
	detail, err := s.BrowserAudioSource(job.Source)
	must(t, err)
	if detail.Message.Transcript.Text != text || detail.Author != "Mariana" || len(detail.Message.Wire) > 0 {
		t.Fatal("detail transcript lost")
	}
	// A first failure must already allow an explicit retry.
	m = audioMessage("failure")
	ingest(t, s, m)
	job, err = s.ClaimAudio(time.Now(), time.Minute)
	must(t, err)
	must(t, s.ReleaseAudio(*job, "transcrição falhou", false))
	detail, err = s.BrowserAudioSource(job.Source)
	must(t, err)
	if !detail.RetryEligible || detail.Attempts != 1 {
		t.Fatal(detail)
	}
	must(t, s.RetryAudio(job.Source))
	detail, err = s.BrowserAudioSource(job.Source)
	must(t, err)
	if detail.Attempts != 0 || detail.State != "pending" {
		t.Fatal(detail)
	}
}
