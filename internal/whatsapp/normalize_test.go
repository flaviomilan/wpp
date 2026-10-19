package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"wpp/internal/archive"
)

func info() types.MessageInfo {
	return types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("group", types.GroupServer), Sender: types.NewJID("sender", types.DefaultUserServer), IsGroup: true}, ID: "message", Timestamp: time.Unix(100, 0)}
}
func TestAudioDescriptorIsPrivateAndMinimal(t *testing.T) {
	audio := &waE2E.AudioMessage{DirectPath: proto.String("/media/audio"), MediaKey: []byte("PRIVATE KEY"), FileSHA256: []byte("hash"), FileEncSHA256: []byte("encrypted hash"), FileLength: proto.Uint64(1024), Mimetype: proto.String("audio/ogg"), Seconds: proto.Uint32(3), Waveform: []byte("waveform"), ContextInfo: &waE2E.ContextInfo{QuotedMessage: &waE2E.Message{Conversation: proto.String("quoted text")}}}
	m, ok, err := Normalize(&events.Message{Info: info(), Message: &waE2E.Message{AudioMessage: audio}})
	if err != nil || !ok || m.Kind != "áudio" || len(m.AudioMetadata) == 0 {
		t.Fatal(m, err)
	}
	var descriptor waE2E.AudioMessage
	if err = proto.Unmarshal(m.AudioMetadata, &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.ContextInfo != nil || len(descriptor.Waveform) > 0 || descriptor.GetDirectPath() != "/media/audio" {
		t.Fatal("descriptor contains extra content")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if _, exists := record["AudioMetadata"]; exists {
		t.Fatal("descriptor exposed in message")
	}
}

func TestGroupDiscoveryRecoversMissingNames(t *testing.T) {
	db, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, db)
	client := &Client{Archive: db, Fail: func(string) {}}
	group := &types.GroupInfo{JID: types.NewJID("120", types.GroupServer)}
	calls := 0
	lookup := func(ctx context.Context, id types.JID) (*types.GroupInfo, error) {
		calls++
		return &types.GroupInfo{JID: id, GroupName: types.GroupName{Name: "Nome recuperado"}}, nil
	}
	if err = client.discoverGroupNames(context.Background(), []*types.GroupInfo{group}, lookup); err != nil {
		t.Fatal(err)
	}
	chats, err := db.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].Name != "Nome recuperado" || calls != 1 {
		t.Fatalf("nome não recuperado: %+v consultas=%d", chats, calls)
	}
}

func TestGroupDiscoveryPreservesKnownNamesOnLookupFailure(t *testing.T) {
	db, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, db)
	if err = db.Discover("120@g.us", "Nome conhecido"); err != nil {
		t.Fatal(err)
	}
	client := &Client{Archive: db, Fail: func(string) {}}
	group := &types.GroupInfo{JID: types.NewJID("120", types.GroupServer)}
	lookup := func(context.Context, types.JID) (*types.GroupInfo, error) { return nil, errors.New("indisponível") }
	if err = client.discoverGroupNames(context.Background(), []*types.GroupInfo{group}, lookup); err != nil {
		t.Fatal(err)
	}
	chats, err := db.Chats()
	if err != nil || chats[0].Name != "Nome conhecido" {
		t.Fatalf("nome anterior perdido: %+v %v", chats, err)
	}
}
func TestTextReplyAndMediaReferences(t *testing.T) {
	ctx := &waE2E.ContextInfo{StanzaID: proto.String("parent"), Participant: proto.String("sender@s.whatsapp.net")}
	for _, m := range []*waE2E.Message{
		{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("first\nsecond"), ContextInfo: ctx}},
		{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("file.pdf"), Caption: proto.String("caption"), ContextInfo: ctx}},
	} {
		normalized, ok, err := Normalize(&events.Message{Info: info(), Message: m})
		if err != nil || !ok || normalized.ReplyID != "parent" {
			t.Fatalf("%+v %v", normalized, err)
		}
		var wire types.MessageInfo
		if err = json.Unmarshal(normalized.Wire, &wire); err != nil || wire.ID != "message" {
			t.Fatal("invalid history cursor")
		}
		if m.DocumentMessage != nil && (normalized.FileName != "file.pdf" || normalized.Text != "caption") {
			t.Fatal("missing media reference")
		}
	}
}
func TestNestedWrappersAndControlMessages(t *testing.T) {
	inner := &waE2E.Message{Conversation: proto.String("text")}
	wrapped := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}}}}
	m, ok, err := Normalize(&events.Message{Info: info(), Message: wrapped})
	if err != nil || !ok || m.Text != "text" {
		t.Fatalf("wrapper: %+v %v", m, err)
	}
	_, ok, err = Normalize(&events.Message{Info: info(), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION.Enum()}}})
	if err != nil || ok {
		t.Fatal("control message archived")
	}
	m, ok, err = Normalize(&events.Message{Info: info()})
	if err != nil || !ok || m.Kind != "indisponível" {
		t.Fatal("unavailable message lost")
	}
}
func TestEditRawEnvelopeAfterHistoryUnwrapping(t *testing.T) {
	key := &waCommon.MessageKey{ID: proto.String("original"), RemoteJID: proto.String("group@g.us"), Participant: proto.String("author@s.whatsapp.net"), FromMe: proto.Bool(false)}
	edit := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: key, TimestampMS: proto.Int64(200000), EditedMessage: &waE2E.Message{Conversation: proto.String("updated")}}}
	raw := &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: edit}}
	m, ok, err := Normalize(&events.Message{Info: info(), RawMessage: raw, Message: edit.ProtocolMessage.EditedMessage, IsEdit: true})
	if err != nil || !ok || !m.Mutation || !m.Edited || m.ID != "original" || m.Text != "updated" || m.Version != 200000 || m.Sender != "author@s.whatsapp.net" {
		t.Fatalf("edit: %+v %v", m, err)
	}
}
func TestRevokeAndHistoricalTombstone(t *testing.T) {
	key := &waCommon.MessageKey{ID: proto.String("original"), RemoteJID: proto.String("group@g.us"), Participant: proto.String("author@s.whatsapp.net")}
	m, ok, err := Normalize(&events.Message{Info: info(), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: key}}})
	if err != nil || !ok || !m.Deleted || !m.Mutation || m.ID != "original" {
		t.Fatalf("revoke: %+v %v", m, err)
	}
	m, ok, err = Normalize(&events.Message{Info: info(), SourceWebMsg: &waWeb.WebMessageInfo{MessageStubType: waWeb.WebMessageInfo_REVOKE.Enum()}})
	if err != nil || !ok || !m.Deleted || m.Mutation {
		t.Fatalf("historical revoke: %+v %v", m, err)
	}
}
func TestHistoryAndLiveShareIngestionPath(t *testing.T) {
	dir := t.TempDir()
	db, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, db)
	if err = db.Track("group@g.us", true); err != nil {
		t.Fatal(err)
	}
	client, err := Open(context.Background(), dir, db, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, client)
	text := &waE2E.Message{Conversation: proto.String("one")}
	web := &waWeb.WebMessageInfo{Key: &waCommon.MessageKey{ID: proto.String("message"), Participant: proto.String("sender@s.whatsapp.net"), FromMe: proto.Bool(false)}, MessageTimestamp: proto.Uint64(100), Message: text}
	history := &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{ID: proto.String("group@g.us"), DisplayName: proto.String("Group"), Messages: []*waHistorySync.HistorySyncMsg{{Message: web}}}}}}
	if !client.event(history) {
		t.Fatal("history ingestion failed")
	}
	if !client.event(&events.Message{Info: info(), Message: text}) {
		t.Fatal("live ingestion failed")
	}
	c, m, _, err := db.Snapshot("group@g.us")
	if err != nil || len(m) != 1 || c.Name != "Group" {
		t.Fatalf("snapshot: %+v %+v %v", c, m, err)
	}
	system := &waWeb.WebMessageInfo{Key: &waCommon.MessageKey{ID: proto.String("system")}, MessageTimestamp: proto.Uint64(90), MessageStubType: waWeb.WebMessageInfo_GROUP_CREATE.Enum()}
	history.Data.Conversations[0].Messages = append(history.Data.Conversations[0].Messages, &waHistorySync.HistorySyncMsg{Message: system})
	if !client.event(history) {
		t.Fatal("system record caused history ingestion failure")
	}
	_, m, _, err = db.Snapshot("group@g.us")
	if err != nil || len(m) != 2 || m[0].Kind != "evento (GROUP_CREATE)" {
		t.Fatalf("system placeholder: %+v %v", m, err)
	}
	// A storage failure must report failure to the synchronous ack handler.
	checkClose(t, db)
	if client.event(&events.Message{Info: info(), Message: text}) {
		t.Fatal("ack success after storage failure")
	}
	select {
	case <-client.Fatal:
	default:
		t.Fatal("missing fatal persistence notification")
	}
}
