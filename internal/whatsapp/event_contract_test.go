package whatsapp

import (
	"errors"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

func TestNormalizeEventEvidenceSeparatesCaptionAndQuotedText(t *testing.T) {
	i := info()
	i.PushName = "Nome observado"
	i.SenderAlt = types.NewJID("123", types.HiddenUserServer)
	ctx := &waE2E.ContextInfo{StanzaID: proto.String("quoted"), Participant: proto.String("other@lid"), QuotedMessage: &waE2E.Message{Conversation: proto.String("texto citado original")}, MentionedJID: []string{"person@lid"}, IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(2)}
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("Contrato original\nsegunda linha"), FileName: proto.String("contrato.pdf"), Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(4096), FileSHA256: []byte{1, 2, 3}, ContextInfo: ctx}}
	m, ok, err := Normalize(&events.Message{Info: i, Message: msg})
	if err != nil || !ok {
		t.Fatal(err)
	}
	e := m.Evidence
	if e == nil || e.ObservedAt == nil || e.Caption == nil || *e.Caption != m.Text || *e.QuotedText != "texto citado original" || !*e.Forwarded || *e.ForwardingScore != 2 || len(e.Mentions) != 1 || len(e.SenderAliases) != 1 || e.NativeSender != m.Sender {
		t.Fatal(e)
	}
	a := e.Attachments[0]
	if *a.Name != "contrato.pdf" || *a.MIME != "application/pdf" || *a.Size != 4096 || *a.SHA256 != "010203" || a.Reference != nil {
		t.Fatal(a)
	}
}
func TestNormalizeReactionSourceTimestampAndSnapshot(t *testing.T) {
	i := info()
	r := &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String("target"), Participant: proto.String("ana@lid"), FromMe: proto.Bool(false)}, Text: proto.String("👨‍👩‍👧‍👦"), SenderTimestampMS: proto.Int64(1792080000123)}
	m, ok, err := Normalize(&events.Message{Info: i, Message: &waE2E.Message{ReactionMessage: r}})
	if err != nil || !ok {
		t.Fatal(err)
	}
	if *m.Evidence.Reaction.Emoji != "👨‍👩‍👧‍👦" || m.Evidence.Reaction.Time.UnixMilli() != r.GetSenderTimestampMS() || m.Evidence.Reaction.TargetID != "target" {
		t.Fatal(m.Evidence)
	}
	web := &waWeb.WebMessageInfo{Reactions: []*waWeb.Reaction{{Key: &waCommon.MessageKey{ID: proto.String("reaction-event"), Participant: proto.String("bia@lid")}, Text: proto.String("✅")}}}
	m, ok, err = Normalize(&events.Message{Info: i, Message: &waE2E.Message{Conversation: proto.String("original")}, SourceWebMsg: web})
	if err != nil || !ok {
		t.Fatal(err)
	}
	if len(m.Evidence.Reactions) != 1 || m.Evidence.Reactions[0].Time != nil || !m.Evidence.ReactionsAvailable {
		t.Fatal(m.Evidence)
	}
	// Missing and empty repeated protobuf fields do not prove an empty reaction list.
	web.Reactions = nil
	m, ok, err = Normalize(&events.Message{Info: i, Message: &waE2E.Message{Conversation: proto.String("original")}, SourceWebMsg: web})
	if err != nil || !ok || m.Evidence.ReactionsAvailable {
		t.Fatal(m, err)
	}
	i.Timestamp = time.Time{}
	m, ok, err = Normalize(&events.Message{Info: i, Message: &waE2E.Message{Conversation: proto.String("sem data")}})
	if err != nil || !ok || m.Evidence.EventTime != nil {
		t.Fatal(m, err)
	}
}

func TestClientDecryptsReactionWhenMessageSecretAvailable(t *testing.T) {
	c, network := adapterFixture(t)
	event := &events.Message{Info: info(), Message: &waE2E.Message{EncReactionMessage: &waE2E.EncReactionMessage{TargetMessageKey: &waCommon.MessageKey{ID: proto.String("original"), Participant: proto.String("ana@lid")}}}}
	network.decrypt = func(m *events.Message) (*waE2E.ReactionMessage, error) {
		if m.Message.GetEncReactionMessage() == nil {
			t.Fatal("encrypted payload lost")
		}
		return &waE2E.ReactionMessage{Key: m.Message.GetEncReactionMessage().GetTargetMessageKey(), Text: proto.String("👍🏽"), SenderTimestampMS: proto.Int64(1792080000000)}, nil
	}
	m, ok, err := c.normalize(event)
	if err != nil || !ok || m.Evidence.Reaction.Emoji == nil || *m.Evidence.Reaction.Emoji != "👍🏽" {
		t.Fatal(m, err)
	}
	network.decrypt = func(*events.Message) (*waE2E.ReactionMessage, error) { return nil, errors.New("secret unavailable") }
	m, ok, err = c.normalize(event)
	if err != nil || !ok || m.Evidence.Reaction.Emoji != nil || m.ReplyID != "original" {
		t.Fatal(m, err)
	}
	network.decrypt = func(*events.Message) (*waE2E.ReactionMessage, error) { return nil, nil }
	m, ok, err = c.normalize(event)
	if err != nil || !ok || m.Evidence.Reaction.Emoji != nil {
		t.Fatal(m, err)
	}
}
