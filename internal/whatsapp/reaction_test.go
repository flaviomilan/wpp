package whatsapp

import (
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
	"wpp/internal/archive"
)

func TestReactionShowsOriginalMessageText(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, store)
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	originalInfo := info()
	originalInfo.PushName = "Ana"
	reactionInfo := originalInfo
	reactionInfo.ID = "reaction"
	reactionInfo.Sender = types.NewJID("reactor", types.DefaultUserServer)
	reactionInfo.PushName = "Bruno"
	reactionInfo.Timestamp = originalInfo.Timestamp.Add(time.Second)
	for _, event := range []*events.Message{
		{Info: originalInfo, Message: &waE2E.Message{Conversation: proto.String("Entrega confirmada para amanhã.")}},
		{Info: reactionInfo, Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String(originalInfo.ID), RemoteJID: proto.String("group@g.us"), Participant: proto.String(originalInfo.Sender.String())}, Text: proto.String("👍")}}},
	} {
		m, ok, err := Normalize(event)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if _, err = store.Ingest([]archive.Message{m}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.BrowseMessages("group@g.us", "", "", 0, 50)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	reaction, original := page.Items[0], page.Items[1]
	if original.Message.Text != "Entrega confirmada para amanhã." {
		t.Fatal("original text lost")
	}
	if reaction.Message.Text != "👍" || reaction.Reply == nil || reaction.Reply.State != "available" || reaction.Reply.Author != "Ana" || reaction.Reply.Preview != original.Message.Text {
		t.Fatalf("reaction hides original text: kind=%q text=%q reference=%+v", reaction.Message.Kind, reaction.Message.Text, reaction.Reply)
	}
}

func TestReactionReplayRecoversLegacyPlaceholder(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, store)
	if err = store.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	old := archive.Message{Chat: "group@g.us", ID: "reaction", Sender: "reactor@s.whatsapp.net", Group: true, Time: time.Unix(100, 0), Kind: "não interpretado (reactionMessage)"}
	if _, err = store.Ingest([]archive.Message{old}); err != nil {
		t.Fatal(err)
	}
	before, err := store.BrowseMessages(old.Chat, "", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Kind, updated.Text, updated.ReplyID = "reação", "✅", "original"
	for i := 0; i < 2; i++ {
		if _, err = store.Ingest([]archive.Message{updated}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := store.BrowseMessages(old.Chat, "", "", 0, 50)
	if err != nil || len(after.Items) != 1 || after.Items[0].Message.Text != "✅" || after.Items[0].SourceID != before.Items[0].SourceID || after.Items[0].Reply.State != "missing" {
		t.Fatal("legacy reaction not recovered", after, err)
	}
}

func TestReactionNormalizationPreservesOwnKeyAndHandlesUnavailableContent(t *testing.T) {
	key := &waCommon.MessageKey{ID: proto.String("original"), Participant: proto.String("author:7@s.whatsapp.net"), RemoteJID: proto.String("group@g.us")}
	for _, test := range []struct {
		message *waE2E.Message
		text    string
	}{
		{&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key, Text: proto.String("")}}, "Reação removida"},
		{&waE2E.Message{EncReactionMessage: &waE2E.EncReactionMessage{TargetMessageKey: key}}, "Reação indisponível"},
	} {
		m, ok, err := Normalize(&events.Message{Info: info(), Message: test.message})
		if err != nil || !ok || m.Kind != "reação" || m.ID != info().ID || m.ReplyID != "original" || m.ReplySender != "author@s.whatsapp.net" || m.Text != test.text || m.Mutation || m.Edited || m.Deleted {
			t.Fatal(m, ok, err)
		}
	}
}
