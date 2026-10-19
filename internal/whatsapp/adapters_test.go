package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"wpp/internal/archive"
)

type fakeNetwork struct {
	decrypt  func(*events.Message) (*waE2E.ReactionMessage, error)
	online   bool
	fail     error
	groups   []*types.GroupInfo
	download func(whatsmeow.File) error
}

func (f *fakeNetwork) IsConnected() bool { return f.online }
func (f *fakeNetwork) DownloadToFile(_ context.Context, _ whatsmeow.DownloadableMessage, out whatsmeow.File) error {
	if f.download != nil {
		return f.download(out)
	}
	return f.fail
}
func (f *fakeNetwork) SendPeerMessage(context.Context, *waE2E.Message) (whatsmeow.SendResponse, error) {
	return whatsmeow.SendResponse{ID: "request"}, f.fail
}
func (*fakeNetwork) BuildHistorySyncRequest(*types.MessageInfo, int) *waE2E.Message {
	return &waE2E.Message{}
}
func (f *fakeNetwork) GetJoinedGroups(context.Context) ([]*types.GroupInfo, error) {
	return f.groups, f.fail
}
func (f *fakeNetwork) GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error) {
	return &types.GroupInfo{}, f.fail
}

type contactStore struct {
	store.NoopStore
	values map[types.JID]types.ContactInfo
	fail   error
}

func (s *contactStore) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return s.values, s.fail
}

func adapterFixture(t *testing.T) (*Client, *fakeNetwork) {
	t.Helper()
	dir := t.TempDir()
	a, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(context.Background(), dir, a, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	f := &fakeNetwork{online: true}
	c.network = f
	return c, f
}
func TestLocalSessionOpenAndClose(t *testing.T) {
	c, _ := adapterFixture(t)
	if c.WA.Store.ID != nil {
		t.Fatal("unexpected linked account")
	}
	if c.protocol() == nil {
		t.Fatal("missing protocol")
	}
	c.network = nil
	if c.protocol() != c.WA {
		t.Fatal("default protocol changed")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.event(&events.Disconnected{}) {
		t.Fatal("closed client accepted event")
	}
	a, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, a)
	if _, err = Open(context.Background(), filepath.Join(t.TempDir(), "missing"), a, nil); err == nil {
		t.Fatal("missing session directory accepted")
	}
}
func TestHistoryProtocolCorrelationAndAliases(t *testing.T) {
	c, f := adapterFixture(t)
	if err := c.Archive.Alias("42@lid", "123@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(types.MessageInfo{ID: "anchor"})
	if err != nil {
		t.Fatal(err)
	}
	anchor := archive.Message{Chat: "42@lid", Wire: wire}
	c.histories <- historyBatch{session: "other"}
	c.histories <- historyBatch{session: "request", messages: []archive.Message{{Chat: "42@lid", Sender: "42@lid", ID: "wanted"}, {Chat: "else@g.us", ID: "unrelated"}}}
	messages, err := c.History(context.Background(), anchor)
	if err != nil || len(messages) != 1 || messages[0].Chat != "123@s.whatsapp.net" || messages[0].Sender != "123@s.whatsapp.net" {
		t.Fatal(messages, err)
	}
	for _, raw := range [][]byte{nil, []byte("invalid")} {
		anchor.Wire = raw
		if _, err = c.History(context.Background(), anchor); err == nil {
			t.Fatal("invalid anchor accepted")
		}
	}
	anchor.Wire = wire
	f.fail = errors.New("network")
	if _, err = c.History(context.Background(), anchor); err == nil {
		t.Fatal("request failure discarded")
	}
	f.fail = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.History(ctx, anchor); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = c.Archive.Close(); err != nil {
		t.Fatal(err)
	}
	c.histories <- historyBatch{session: "request"}
	if _, err = c.History(context.Background(), anchor); err == nil {
		t.Fatal("closed archive accepted")
	}
}
func TestDiscoverGroupsAndContacts(t *testing.T) {
	c, f := adapterFixture(t)
	f.groups = []*types.GroupInfo{{JID: types.NewJID("group", types.GroupServer), GroupName: types.GroupName{Name: "Team"}, Participants: []types.GroupParticipant{{JID: types.NewJID("42", types.HiddenUserServer), PhoneNumber: types.NewJID("123", types.DefaultUserServer), DisplayName: "Alice"}}}}
	contacts := &contactStore{values: map[types.JID]types.ContactInfo{types.NewJID("123", types.DefaultUserServer): {FullName: "Alice"}, types.NewJID("456", types.DefaultUserServer): {PushName: "Bob"}, types.NewJID("skip", types.GroupServer): {}, types.NewJID("skip", "broadcast"): {}}}
	c.WA.Store.Contacts = contacts
	if err := c.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	canonical, err := c.Archive.Canonical("42@lid")
	if err != nil || canonical != "123@s.whatsapp.net" {
		t.Fatal(canonical, err)
	}
	f.fail = errors.New("private")
	if err = c.Discover(context.Background()); err == nil || err.Error() == "private" {
		t.Fatal(err)
	}
	f.fail = nil
	contacts.fail = errors.New("contacts")
	if err = c.Discover(context.Background()); err == nil {
		t.Fatal("contact failure lost")
	}
	contacts.fail = nil
	if err = c.Archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = c.Discover(context.Background()); err == nil {
		t.Fatal("storage failure lost")
	}
}
func TestDownloadAudioLimitsAndFailures(t *testing.T) {
	c, f := adapterFixture(t)
	path := filepath.Join(t.TempDir(), "audio")
	if !c.AudioReady() {
		t.Fatal("not ready")
	}
	if err := c.DownloadAudio(context.Background(), []byte{255}, path); err == nil {
		t.Fatal("bad descriptor accepted")
	}
	large, err := proto.Marshal(&waE2E.AudioMessage{FileLength: proto.Uint64(maxAudioBytes + 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.DownloadAudio(context.Background(), large, path); err == nil {
		t.Fatal("large descriptor accepted")
	}
	raw, err := proto.Marshal(&waE2E.AudioMessage{FileLength: proto.Uint64(4)})
	if err != nil {
		t.Fatal(err)
	}
	f.online = false
	if c.AudioReady() {
		t.Fatal("offline ready")
	}
	if err = c.DownloadAudio(context.Background(), raw, path); !errors.Is(err, archive.ErrAudioOffline) {
		t.Fatal(err)
	}
	f.online = true
	f.download = func(out whatsmeow.File) error { _, err := out.Write([]byte("test")); return err }
	if err = c.DownloadAudio(context.Background(), raw, path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "test" {
		t.Fatal(string(contents), err)
	}
	if err = c.DownloadAudio(context.Background(), raw, path); err == nil {
		t.Fatal("existing file overwritten")
	}
	f.download = func(whatsmeow.File) error { return errors.New("private") }
	if err = c.DownloadAudio(context.Background(), raw, path+"2"); err == nil || err.Error() == "private" {
		t.Fatal(err)
	}
	f.download = func(whatsmeow.File) error { f.online = false; return errors.New("offline") }
	if err = c.DownloadAudio(context.Background(), raw, path+"3"); !errors.Is(err, archive.ErrAudioOffline) {
		t.Fatal(err)
	}
}
func TestLimitedAudioFile(t *testing.T) {
	raw, err := os.CreateTemp(t.TempDir(), "audio")
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, raw)
	f := limitedAudioFile{raw}
	for _, n := range []int64{-1, maxAudioBytes + 65} {
		if err = f.Truncate(n); err == nil {
			t.Fatal("invalid truncate", n)
		}
		if _, err = f.WriteAt([]byte("x"), n); err == nil {
			t.Fatal("invalid write", n)
		}
	}
	if err = f.Truncate(4); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("xy"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(maxAudioBytes+64, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("x")); err == nil {
		t.Fatal("large write accepted")
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("x")); err == nil {
		t.Fatal("closed write accepted")
	}
}
func TestLifecycleAndGroupEvents(t *testing.T) {
	for _, event := range []any{&events.LoggedOut{}, &events.TemporaryBan{}, &events.StreamReplaced{}, &events.ConnectFailure{}, &events.ClientOutdated{}, &events.Connected{}} {
		t.Run("fatal", func(t *testing.T) {
			c, _ := adapterFixture(t)
			if c.event(event) {
				t.Fatal("fatal event acknowledged")
			}
			select {
			case <-c.Fatal:
			default:
				t.Fatal("missing fatal event")
			}
			c.fatal(errors.New("second"))
			c.fatal(errors.New("overflow"))
		})
	}
	c, _ := adapterFixture(t)
	id := types.NewJID("account", types.DefaultUserServer)
	c.WA.Store.ID = &id
	c.WA.Store.LID = types.NewJID("42", types.HiddenUserServer)
	first := c.event(&events.Connected{})
	second := c.event(&events.Connected{})
	if !first || !second {
		t.Fatal("connection rejected")
	}
	if len(c.Online) != 1 {
		t.Fatal("online signals not bounded")
	}
	first = c.event(&events.Disconnected{})
	second = c.event(&events.Disconnected{})
	if !first || !second || len(c.Offline) != 1 {
		t.Fatal("offline signals")
	}
	if !c.event(&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: types.NewJID("group", types.GroupServer), GroupName: types.GroupName{Name: "Team"}}}) {
		t.Fatal("group rejected")
	}
	if !c.event(&events.GroupInfo{JID: types.NewJID("group", types.GroupServer), Name: &types.GroupName{Name: "Renamed"}}) {
		t.Fatal("group name rejected")
	}
	if !c.event(&events.GroupInfo{}) || !c.event(&events.HistorySync{}) {
		t.Fatal("empty event rejected")
	}
	if !c.event(&events.UndecryptableMessage{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("group", types.GroupServer), IsGroup: true, Sender: id}, ID: "missing", Timestamp: time.Now()}}) {
		t.Fatal("placeholder rejected")
	}
	if err := c.Archive.Close(); err != nil {
		t.Fatal(err)
	}
	if c.event(&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: types.NewJID("group", types.GroupServer)}}) {
		t.Fatal("storage failure acknowledged")
	}
}

type lidStore struct {
	store.NoopStore
	fail error
	pn   types.JID
}

func (s *lidStore) GetPNForLID(context.Context, types.JID) (types.JID, error) { return s.pn, s.fail }
func TestAliasSourcesAndFailures(t *testing.T) {
	c, _ := adapterFixture(t)
	phone := types.NewJID("123", types.DefaultUserServer)
	hidden := types.NewJID("42", types.HiddenUserServer)
	for _, own := range []bool{false, true} {
		message := types.MessageInfo{MessageSource: types.MessageSource{Chat: phone, Sender: phone, SenderAlt: hidden, RecipientAlt: hidden, IsFromMe: own}}
		if err := c.aliasSource(message); err != nil {
			t.Fatal(err)
		}
	}
	c.WA.Store.LIDs = &lidStore{pn: phone}
	if err := c.aliasSource(types.MessageInfo{MessageSource: types.MessageSource{Chat: hidden, Sender: hidden}}); err != nil {
		t.Fatal(err)
	}
	c.WA.Store.LIDs = &lidStore{fail: errors.New("mapping")}
	if err := c.aliasSource(types.MessageInfo{MessageSource: types.MessageSource{Chat: hidden}}); err == nil {
		t.Fatal("mapping failure discarded")
	}
	c.WA.Store.LIDs = &lidStore{pn: phone}
	if err := c.Archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.aliasSource(types.MessageInfo{MessageSource: types.MessageSource{Chat: hidden}}); err == nil {
		t.Fatal("mapping storage failure discarded")
	}
	if err := c.aliasSource(types.MessageInfo{MessageSource: types.MessageSource{Sender: hidden, SenderAlt: phone}}); err == nil {
		t.Fatal("alternate storage failure discarded")
	}
}
func TestEventStorageFailuresAndUnsupportedMessages(t *testing.T) {
	for _, kind := range []string{"connected", "name", "undecryptable", "history", "message", "alias", "interpret"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := adapterFixture(t)
			i := info()
			var event any
			switch kind {
			case "connected":
				id := types.NewJID("123", types.DefaultUserServer)
				c.WA.Store.ID = &id
				event = &events.Connected{}
			case "name":
				event = &events.GroupInfo{JID: types.NewJID("group", types.GroupServer), Name: &types.GroupName{Name: "New"}}
			case "undecryptable":
				event = &events.UndecryptableMessage{Info: i}
			case "history":
				event = &events.HistorySync{Data: &waHistorySync.HistorySync{}}
			case "message":
				event = &events.Message{Info: i, Message: &waE2E.Message{Conversation: proto.String("message")}}
			case "alias":
				i.Sender = types.NewJID("42", types.HiddenUserServer)
				i.SenderAlt = types.NewJID("123", types.DefaultUserServer)
				event = &events.UndecryptableMessage{Info: i}
			case "interpret":
				event = &events.Message{Info: i, Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum()}}}
			}
			if kind != "interpret" {
				if err := c.Archive.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if c.event(event) {
				t.Fatal("failure acknowledged")
			}
			select {
			case <-c.Fatal:
			default:
				t.Fatal("failure not reported")
			}
		})
	}
	c, _ := adapterFixture(t)
	if !c.event(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("status", "broadcast")}}}) {
		t.Fatal("irrelevant message caused failure")
	}
	for i := 0; i < cap(c.Activity); i++ {
		c.Activity <- "filled"
	}
	if !c.event(&events.Message{Info: info(), Message: &waE2E.Message{Conversation: proto.String("message")}}) {
		t.Fatal("activity overflow blocked ingest")
	}
}
func TestHistoryMappingsAndRequestQueue(t *testing.T) {
	c, _ := adapterFixture(t)
	if err := c.Archive.Track("123@s.whatsapp.net", true); err != nil {
		t.Fatal(err)
	}
	raw := &waWeb.WebMessageInfo{Key: &waCommon.MessageKey{ID: proto.String("one"), RemoteJID: proto.String("123@s.whatsapp.net"), FromMe: proto.Bool(false)}, MessageTimestamp: proto.Uint64(100), Message: &waE2E.Message{Conversation: proto.String("Hello")}}
	event := &events.HistorySync{Data: &waHistorySync.HistorySync{PhoneNumberToLidMappings: []*waHistorySync.PhoneNumberToLIDMapping{{LidJID: proto.String("42@lid"), PnJID: proto.String("123@s.whatsapp.net")}}, Conversations: []*waHistorySync.Conversation{{ID: proto.String("status@broadcast")}, {ID: proto.String("123@s.whatsapp.net"), LidJID: proto.String("42@lid"), PnJID: proto.String("123@s.whatsapp.net"), Name: proto.String("Alice"), Messages: []*waHistorySync.HistorySyncMsg{{Message: raw}}}}}, Notification: &waE2E.HistorySyncNotification{PeerDataRequestSessionID: proto.String("request")}}
	if err := c.history(event); err != nil {
		t.Fatal(err)
	}
	if batch := <-c.histories; batch.session != "request" || len(batch.messages) != 1 {
		t.Fatal(batch)
	}
	for i := 0; i < cap(c.histories); i++ {
		c.histories <- historyBatch{}
	}
	for i := len(c.Activity); i < cap(c.Activity); i++ {
		c.Activity <- "filled"
	}
	notices := 0
	c.Fail = func(string) { notices++ }
	if err := c.history(event); err != nil || notices != 1 {
		t.Fatal(err, notices)
	}
	raw.Key.Participant = proto.String("bad:jid@g.us")
	raw.Key.RemoteJID = nil
	event.Data.Conversations[1].ID = proto.String("group@g.us")
	if err := c.history(event); err == nil {
		t.Fatal("unparseable message accepted")
	}
	if err := c.Archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.history(event); err == nil {
		t.Fatal("mapping storage failure ignored")
	}
	event.Data.PhoneNumberToLidMappings = nil
	if err := c.history(event); err == nil {
		t.Fatal("conversation mapping failure ignored")
	}
	event.Data.Conversations[1].LidJID = nil
	event.Data.Conversations[1].PnJID = nil
	if err := c.history(event); err == nil {
		t.Fatal("conversation persistence failure ignored")
	}
}
func TestDiscoverGroupIdentityVariants(t *testing.T) {
	c, _ := adapterFixture(t)
	groups := []*types.GroupInfo{nil, {JID: types.NewJID("group", types.GroupServer), GroupName: types.GroupName{Name: "Team"}, Participants: []types.GroupParticipant{{JID: types.NewJID("123", types.DefaultUserServer), LID: types.NewJID("42", types.HiddenUserServer)}, {PhoneNumber: types.NewJID("456", types.DefaultUserServer)}, {LID: types.NewJID("43", types.HiddenUserServer)}}}}
	if err := c.discoverGroupNames(context.Background(), groups, func(context.Context, types.JID) (*types.GroupInfo, error) {
		t.Fatal("named group queried")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.discoverGroupNames(ctx, []*types.GroupInfo{{JID: types.NewJID("group", types.GroupServer)}}, func(context.Context, types.JID) (*types.GroupInfo, error) { return nil, ctx.Err() }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNormalizeMediaTypesAndWrappers(t *testing.T) {
	quoted := &waE2E.ContextInfo{StanzaID: proto.String("original"), Participant: proto.String("sender@s.whatsapp.net")}
	for _, test := range []struct {
		message    *waE2E.Message
		kind, text string
	}{
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("Photo"), ContextInfo: quoted}}, "imagem", "Photo"},
		{&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("Video"), ContextInfo: quoted}}, "vídeo", "Video"},
		{&waE2E.Message{StickerMessage: &waE2E.StickerMessage{ContextInfo: quoted}}, "figurinha", ""},
		{&waE2E.Message{LocationMessage: &waE2E.LocationMessage{Name: proto.String("Home"), Address: proto.String("Street"), DegreesLatitude: proto.Float64(1), DegreesLongitude: proto.Float64(2), ContextInfo: quoted}}, "localização", "Home Street (1.000000, 2.000000)"},
		{&waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Alice"), ContextInfo: quoted}}, "contato", "Alice"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			i := info()
			i.Timestamp = time.Time{}
			m, ok, err := Normalize(&events.Message{Info: i, Message: test.message})
			if err != nil || !ok || m.Kind != test.kind || m.Text != test.text || m.ReplyID != "original" || !m.Time.Equal(time.Unix(0, 0)) {
				t.Fatal(m, err)
			}
		})
	}
	inner := &waE2E.Message{Conversation: proto.String("Wrapped")}
	for _, outer := range []*waE2E.Message{
		{ViewOnceMessage: &waE2E.FutureProofMessage{Message: inner}},
		{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}},
		{ViewOnceMessageV2Extension: &waE2E.FutureProofMessage{Message: inner}},
		{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: inner}},
		{EditedMessage: &waE2E.FutureProofMessage{Message: inner}},
		{DeviceSentMessage: &waE2E.DeviceSentMessage{Message: inner}},
	} {
		m, ok, err := Normalize(&events.Message{Info: info(), Message: outer})
		if err != nil || !ok || m.Text != "Wrapped" {
			t.Fatal(m, err)
		}
	}
}
func TestNormalizeMutationKeysAndReactionBoundaries(t *testing.T) {
	for _, test := range []struct {
		key       *waCommon.MessageKey
		wantError bool
	}{
		{nil, true},
		{&waCommon.MessageKey{ID: proto.String("original"), RemoteJID: proto.String("bad:jid@g.us")}, true},
		{&waCommon.MessageKey{ID: proto.String("original"), Participant: proto.String("bad:jid@g.us")}, true},
		{&waCommon.MessageKey{ID: proto.String("original"), RemoteJID: proto.String("group@g.us"), Participant: proto.String("other@s.whatsapp.net")}, false},
	} {
		m, ok, err := Normalize(&events.Message{Info: info(), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: test.key}}})
		if (err != nil) != test.wantError {
			t.Fatal(m, err)
		}
		if !test.wantError && (!ok || !m.Edited || m.Sender != "other@s.whatsapp.net") {
			t.Fatal(m, err)
		}
	}
	for _, key := range []*waCommon.MessageKey{nil, {RemoteJID: proto.String("else@g.us"), ID: proto.String("original")}, {RemoteJID: proto.String("bad:jid@g.us"), ID: proto.String("original")}} {
		m, ok, err := Normalize(&events.Message{Info: info(), Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key}}})
		if err != nil || !ok || m.Text != "Reação removida" || m.ReplyID != "" {
			t.Fatal(m, err)
		}
	}
	unsupported := info()
	unsupported.Chat = types.NewJID("status", "broadcast")
	if _, ok, err := Normalize(&events.Message{Info: unsupported}); ok || err != nil {
		t.Fatal("unsupported chat accepted", err)
	}
	m, ok, err := Normalize(&events.Message{Info: info(), Message: &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String("Poll")}}})
	if err != nil || !ok || m.Kind != "não interpretado (pollCreationMessage)" {
		t.Fatal(m, err)
	}
}

func checkClose(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Error(err)
	}
}

func (f *fakeNetwork) DecryptReaction(_ context.Context, m *events.Message) (*waE2E.ReactionMessage, error) {
	if f.decrypt != nil {
		return f.decrypt(m)
	}
	return nil, errors.New("message secret unavailable")
}
