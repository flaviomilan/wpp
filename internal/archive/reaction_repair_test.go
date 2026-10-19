package archive

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRepairsLegacyReactionWithoutInventingContent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.BindAccount("account@s.whatsapp.net"))
	must(t, s.Track("group@g.us", true))
	original := Message{Chat: "group@g.us", ID: "original", Sender: "author@lid", Group: true, Kind: "texto", Text: "Entrega confirmada", Time: time.Unix(100, 0)}
	legacy := Message{Chat: original.Chat, ID: "reaction", Sender: "reactor@lid", Group: true, Kind: "não interpretado (reactionMessage)", Time: time.Unix(101, 0)}
	_, err = s.Ingest([]Message{original, legacy})
	must(t, err)
	before, err := s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	must(t, s.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	after, err := s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	if len(after.Items) != 2 || after.Items[0].Message.Kind != "reação" || after.Items[0].Message.Text != "Reação indisponível" {
		t.Fatal("legacy record still incorrect", after)
	}
	if after.Items[0].SourceID != before.Items[0].SourceID || after.Items[0].Message.ReplyID != "" || after.Items[1].Message.Text != original.Text {
		t.Fatal("original changed or reference invented")
	}
	var changes int
	must(t, s.db.QueryRow("select count(*) from changes").Scan(&changes))
	if changes != 3 {
		t.Fatal("repair did not create one revision", changes)
	}
	b := EventBatchService{Store: s, Dir: filepath.Join(dir, "exports")}
	path, err := b.Next(original.Chat)
	must(t, err)
	if path == "" {
		t.Fatal("repair evidence not available to analysis")
	}
	must(t, s.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	var repeated int
	must(t, s.db.QueryRow("select count(*) from changes").Scan(&repeated))
	if repeated != changes {
		t.Fatal("repair is not idempotent", repeated)
	}
	// An encrypted replay may recover the target before the emoji is available.
	replay := after.Items[0].Message
	replay.ReplyID, replay.ReplySender = original.ID, original.Sender
	_, err = s.Ingest([]Message{replay})
	must(t, err)
	resolved, err := s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	if resolved.Items[0].SourceID != before.Items[0].SourceID || resolved.Items[0].Reply == nil || resolved.Items[0].Reply.State != "available" || resolved.Items[0].Reply.Preview != original.Text {
		t.Fatal("replayed target not recovered", resolved)
	}
}
