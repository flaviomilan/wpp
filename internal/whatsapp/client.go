package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"wpp/internal/localization"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"wpp/internal/archive"
)

type historyBatch struct {
	session  string
	messages []archive.Message
}

var configureDevice sync.Once

var ErrSessionRevoked = localization.New("archive.session_revoked_run_wpp_login_again")

// networkClient contains only protocol operations that require a connection.
// Persistence and normalization continue through the concrete local client.
type networkClient interface {
	IsConnected() bool
	DownloadToFile(context.Context, whatsmeow.DownloadableMessage, whatsmeow.File) error
	SendPeerMessage(context.Context, *waE2E.Message) (whatsmeow.SendResponse, error)
	BuildHistorySyncRequest(*types.MessageInfo, int) *waE2E.Message
	GetJoinedGroups(context.Context) ([]*types.GroupInfo, error)
	GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error)
}

type Client struct {
	network   networkClient
	WA        *whatsmeow.Client
	container *sqlstore.Container
	Archive   *archive.Store
	Online    chan struct{}
	Offline   chan struct{}
	Activity  chan string
	Fatal     chan error
	Fail      func(string)
	mu        sync.Mutex
	histories chan historyBatch
	historyMu sync.Mutex
	stopping  bool
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, dir string, a *archive.Store, fail func(string)) (*Client, error) {
	path, err := filepath.Abs(filepath.Join(dir, "session.db"))
	if err != nil {
		return nil, err
	}
	container, err := sqlstore.New(ctx, "sqlite3", (&url.URL{Scheme: "file", Path: path}).String()+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL", waLog.Noop)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, errors.Join(err, container.Close())
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, errors.Join(err, container.Close())
	}
	if device.ID != nil {
		if err = a.BindAccount(jid(*device.ID)); err != nil {
			return nil, errors.Join(err, container.Close())
		}
	}
	configureDevice.Do(func() {
		store.DeviceProps.RequireFullSync = proto.Bool(true)
		store.DeviceProps.HistorySyncConfig.OnDemandReady = proto.Bool(true)
		store.SetOSInfo("wpp", [3]uint32{1, 0, 0})
	})
	c := &Client{WA: whatsmeow.NewClient(device, waLog.Noop), container: container, Archive: a, Online: make(chan struct{}, 1), Offline: make(chan struct{}, 1), Activity: make(chan string, 256), Fatal: make(chan error, 1), Fail: fail, histories: make(chan historyBatch, 256)}
	// Reconnection is provided by whatsmeow, with increasing waits on failure.
	c.WA.EnableAutoReconnect = true
	c.WA.SynchronousAck = true
	c.WA.AddEventHandlerWithSuccessStatus(c.event)
	return c, nil
}
func (c *Client) protocol() networkClient {
	if c.network != nil {
		return c.network
	}
	return c.WA
}

func (c *Client) fatal(err error) {
	c.stopping = true
	select {
	case c.Fatal <- err:
	default:
	}
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.WA.Disconnect()
		c.mu.Lock()
		c.stopping = true
		c.mu.Unlock()
		c.closeErr = c.container.Close()
	})
	return c.closeErr
}
func (c *Client) event(evt any) (success bool) {
	// Serializing callbacks also ensures Close waits for accepted persistence.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopping {
		return
	}
	defer func() { success = !c.stopping }()
	switch e := evt.(type) {
	case *events.Connected:
		if c.WA.Store.ID == nil {
			c.fatal(localization.New("archive.session_has_no_identifier"))
			return
		}
		if err := c.Archive.BindAccount(jid(*c.WA.Store.ID)); err != nil {
			c.fatal(err)
			return
		}
		if !c.WA.Store.LID.IsEmpty() {
			if err := c.Archive.Alias(jid(c.WA.Store.LID), jid(*c.WA.Store.ID)); err != nil {
				c.fatal(localization.New("archive.failed_to_save_account_identity"))
				return
			}
		}
		select {
		case c.Online <- struct{}{}:
		default:
		}
	case *events.LoggedOut:
		c.fatal(ErrSessionRevoked)
	case *events.TemporaryBan:
		c.fatal(localization.New("archive.connection_suspended_by_whatsapp_capture_stopped"))
	case *events.StreamReplaced:
		c.fatal(localization.New("archive.session_connected_in_another_process_capture_stopped"))
	case *events.ConnectFailure:
		c.fatal(localization.New("archive.authentication_or_connection_failed_check_the_session"))
	case *events.ClientOutdated:
		c.fatal(localization.New("archive.incompatible_client_update_the_whatsmeow_dependency"))
	case *events.Disconnected:
		select {
		case c.Offline <- struct{}{}:
		default:
		}
	case *events.Message:
		if err := c.aliasSource(e.Info); err != nil {
			c.fatal(localization.New("archive.failed_to_save_identities"))
			return
		}
		m, ok, err := c.normalize(e)
		if err != nil {
			c.fatal(localization.New("archive.failed_to_interpret_message"))
			return
		}
		if ok {
			if _, err = c.Archive.Ingest([]archive.Message{m}); err != nil {
				c.fatal(localization.New("archive.failed_to_save_message_capture_stopped"))
			} else {
				select {
				case c.Activity <- m.Chat:
				default:
				}
			}
		}
	case *events.UndecryptableMessage:
		if err := c.aliasSource(e.Info); err != nil {
			c.fatal(localization.New("archive.failed_to_save_identities"))
			return
		}
		m, ok, err := Normalize(&events.Message{Info: e.Info})
		if err == nil && ok {
			_, err = c.Archive.Ingest([]archive.Message{m})
		}
		if err != nil {
			c.fatal(localization.New("archive.failed_to_save_unavailable_message"))
		}
	case *events.HistorySync:
		if err := c.history(e); err != nil {
			c.fatal(localization.New("archive.failed_to_save_history_capture_stopped"))
		}
	case *events.JoinedGroup:
		if err := c.Archive.Discover(jid(e.JID), e.Name); err != nil {
			c.fatal(localization.New("archive.failed_to_save_group"))
		}
	case *events.GroupInfo:
		if e.Name != nil {
			if err := c.Archive.Discover(jid(e.JID), e.Name.Name); err != nil {
				c.fatal(localization.New("archive.failed_to_update_group_name"))
			}
		}
	}
	return
}

func (c *Client) aliasSource(info types.MessageInfo) error {
	pairs := [][2]types.JID{{info.Sender, info.SenderAlt}}
	if !info.IsGroup {
		if info.IsFromMe {
			pairs = append(pairs, [2]types.JID{info.Chat, info.RecipientAlt})
		} else {
			pairs = append(pairs, [2]types.JID{info.Chat, info.SenderAlt})
		}
	}
	for _, p := range pairs {
		a, b := p[0], p[1]
		if a.Server == types.DefaultUserServer && b.Server == types.HiddenUserServer {
			a, b = b, a
		}
		if a.Server == types.HiddenUserServer && b.Server == types.DefaultUserServer {
			if err := c.Archive.Alias(jid(a), jid(b)); err != nil {
				return err
			}
		}
	}
	// History records don't always include alternate IDs. Use mappings already
	// retained by whatsmeow, including the linked account's own identity.
	for _, j := range []types.JID{info.Chat, info.Sender} {
		if j.Server != types.HiddenUserServer {
			continue
		}
		pn, err := c.WA.Store.LIDs.GetPNForLID(context.Background(), j.ToNonAD())
		if err != nil {
			return err
		}
		if !pn.IsEmpty() {
			if err = c.Archive.Alias(jid(j), jid(pn)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) history(e *events.HistorySync) error {
	if e.Data == nil {
		return nil
	}
	for _, p := range e.Data.GetPhoneNumberToLidMappings() {
		if err := c.Archive.Alias(p.GetLidJID(), p.GetPnJID()); err != nil {
			return err
		}
	}
	var batch []archive.Message
	for _, conv := range e.Data.GetConversations() {
		if conv.GetLidJID() != "" && conv.GetPnJID() != "" {
			if err := c.Archive.Alias(conv.GetLidJID(), conv.GetPnJID()); err != nil {
				return err
			}
		}
		chat, err := types.ParseJID(conv.GetID())
		if err != nil || !Supported(chat) {
			continue
		}
		name := strings.TrimSpace(conv.GetName())
		if name == "" {
			name = strings.TrimSpace(conv.GetDisplayName())
		}
		if err = c.Archive.Discover(jid(chat), name); err != nil {
			return err
		}
		for _, item := range conv.GetMessages() {
			evt, err := c.WA.ParseWebMessage(chat, item.GetMessage())
			if err != nil {
				web := item.GetMessage()
				// System records can legitimately lack a participant. Preserve
				// the placeholder without discarding the entire history batch.
				if web.GetMessageStubType() == waWeb.WebMessageInfo_UNKNOWN || web.GetKey().GetID() == "" {
					return err
				}
				evt = &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, IsGroup: chat.Server == types.GroupServer, IsFromMe: web.GetKey().GetFromMe()}, ID: web.GetKey().GetID(), Timestamp: time.Unix(int64(web.GetMessageTimestamp()), 0), PushName: "Sistema"}, Message: web.GetMessage(), RawMessage: web.GetMessage(), SourceWebMsg: web}
			}
			if err = c.aliasSource(evt.Info); err != nil {
				return err
			}
			m, ok, err := c.normalize(evt)
			if err != nil {
				return err
			}
			if ok {
				if m.Evidence != nil {
					m.Evidence.Origin = "history"
				}
				batch = append(batch, m)
			}
		}
	}
	if _, err := c.Archive.Ingest(batch); err != nil {
		return err
	}
	for _, m := range batch {
		select {
		case c.Activity <- m.Chat:
		default:
		}
	}
	if e.Notification != nil && e.Notification.GetPeerDataRequestSessionID() != "" {
		select {
		case c.histories <- historyBatch{session: e.Notification.GetPeerDataRequestSessionID(), messages: batch}:
		default:
			if c.Fail != nil {
				c.Fail("archive.history_queue_full")
			}
		}
	}
	return nil
}

// History correlates responses with their request ID; unrelated sync batches
// are still archived by the event handler but cannot satisfy this request.
func (c *Client) History(ctx context.Context, anchor archive.Message) ([]archive.Message, error) {
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	var info types.MessageInfo
	if len(anchor.Wire) == 0 {
		return nil, localization.New("archive.no_protocol_reference")
	}
	if err := json.Unmarshal(anchor.Wire, &info); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	response, err := c.protocol().SendPeerMessage(ctx, c.protocol().BuildHistorySyncRequest(&info, 50))
	if err != nil {
		return nil, localization.New("archive.history_request_failed")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case batch := <-c.histories:
			if batch.session != response.ID {
				continue
			}
			var messages []archive.Message
			target, err := c.Archive.Canonical(anchor.Chat)
			if err != nil {
				return nil, err
			}
			for _, m := range batch.messages {
				id, e := c.Archive.Canonical(m.Chat)
				if e != nil {
					return nil, e
				}
				if id == target {
					m.Chat = id
					m.Sender, e = c.Archive.Canonical(m.Sender)
					if e != nil {
						return nil, e
					}
					messages = append(messages, m)
				}
			}
			return messages, nil
		}
	}
}

func (c *Client) Discover(ctx context.Context) error {
	groups, err := c.protocol().GetJoinedGroups(ctx)
	if err != nil {
		return localization.New("archive.could_not_list_groups")
	}
	if err = c.discoverGroupNames(ctx, groups, c.protocol().GetGroupInfo); err != nil {
		return err
	}
	contacts, err := c.WA.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return err
	}
	for j, contact := range contacts {
		if !Supported(j) || j.Server == types.GroupServer {
			continue
		}
		name := contact.FullName
		if name == "" {
			name = contact.PushName
		}
		if err = c.Archive.Discover(jid(j), name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) discoverGroupNames(ctx context.Context, groups []*types.GroupInfo, lookup func(context.Context, types.JID) (*types.GroupInfo, error)) error {
	for _, g := range groups {
		if g == nil {
			continue
		}
		name := strings.TrimSpace(g.Name)
		if name == "" {
			queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			details, err := lookup(queryCtx, g.JID)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil && details != nil {
				name = strings.TrimSpace(details.Name)
				g.Participants = details.Participants
			}
			if name == "" && c.Fail != nil {
				c.Fail("archive.group_name_unavailable")
			}
		}
		if err := c.Archive.Discover(jid(g.JID), name); err != nil {
			return err
		}
		for _, p := range g.Participants {
			lid := p.LID
			phone := p.PhoneNumber
			if lid.IsEmpty() && p.JID.Server == types.HiddenUserServer {
				lid = p.JID
			}
			if phone.IsEmpty() && p.JID.Server == types.DefaultUserServer {
				phone = p.JID
			}
			if !lid.IsEmpty() && !phone.IsEmpty() {
				if err := c.Archive.Alias(jid(lid), jid(phone)); err != nil {
					return err
				}
			}
			id := jid(p.JID)
			if id == "" {
				id = jid(phone)
			}
			if id == "" {
				id = jid(lid)
			}
			if err := c.Archive.ObservePerson(jid(g.JID), id, p.DisplayName); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalize attempts the library's local decryption when a reaction uses a message secret.
// Missing secrets preserve the encrypted observation instead of dropping it or logging private data.
func (c *Client) normalize(evt *events.Message) (archive.Message, bool, error) {
	copyEvent := *evt
	if content := unwrap(evt.Message); content.GetEncReactionMessage() != nil {
		if decryptor, ok := c.protocol().(interface {
			DecryptReaction(context.Context, *events.Message) (*waE2E.ReactionMessage, error)
		}); ok {
			encrypted := copyEvent
			encrypted.Message = content
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			reaction, err := decryptor.DecryptReaction(ctx, &encrypted)
			cancel()
			if err == nil && reaction != nil {
				copyEvent.Message = &waE2E.Message{ReactionMessage: reaction}
			}
		}
	}
	return Normalize(&copyEvent)
}
