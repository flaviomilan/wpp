package whatsapp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"wpp/internal/archive"
)

func jid(j types.JID) string {
	if j.IsEmpty() {
		return ""
	}
	return j.ToNonAD().String()
}
func Supported(j types.JID) bool {
	return j.Server == types.GroupServer || j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer
}

func unwrap(m *waE2E.Message) *waE2E.Message {
	for i := 0; m != nil && i < 12; i++ {
		switch {
		case m.GetEphemeralMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetDocumentWithCaptionMessage() != nil:
			m = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetEditedMessage() != nil:
			m = m.GetEditedMessage().GetMessage()
		case m.GetDeviceSentMessage() != nil:
			m = m.GetDeviceSentMessage().GetMessage()
		default:
			return m
		}
	}
	return m
}

func Normalize(evt *events.Message) (archive.Message, bool, error) {
	info := evt.Info
	if !Supported(info.Chat) {
		return archive.Message{}, false, nil
	}
	info.Chat = info.Chat.ToNonAD()
	info.Sender = info.Sender.ToNonAD()
	wire, err := json.Marshal(info)
	if err != nil {
		return archive.Message{}, false, err
	}
	m := archive.Message{Chat: jid(info.Chat), ID: info.ID, Sender: jid(info.Sender), Author: info.PushName, FromMe: info.IsFromMe, Group: info.IsGroup, Time: info.Timestamp.UTC(), Kind: "indisponível", Wire: wire}
	now := time.Now().UTC()
	m.Evidence = &archive.Observation{ObservedAt: &now, EventTime: sourceTime(info.Timestamp), EventMessageID: info.ID, Origin: "live", NativeSender: jid(info.Sender), Mentions: []string{}, Attachments: []archive.Attachment{}}
	if !info.SenderAlt.IsEmpty() {
		m.Evidence.SenderAliases = []string{jid(info.SenderAlt)}
	}
	if evt.SourceWebMsg != nil {
		m.Evidence.Origin = "recovered"
		m.Evidence.ReactionsAvailable = len(evt.SourceWebMsg.GetReactions()) > 0
		m.Evidence.ReactionsCompleteness = "unknown"
		for _, r := range evt.SourceWebMsg.GetReactions() {
			actor := r.GetKey().GetParticipant()
			if r.GetKey().GetFromMe() {
				actor = ""
			} // resolved against the bound account by the archive
			m.Evidence.Reactions = append(m.Evidence.Reactions, archive.ReactionObservation{EventMessageID: r.GetKey().GetID(), ActorID: actor, ActorIsAccount: r.GetKey().GetFromMe(), TargetID: m.ID, TargetSender: m.Sender, TargetFromMe: m.FromMe, Emoji: r.Text, Time: millisTime(r.GetSenderTimestampMS())})
		}
	}
	content := unwrap(evt.Message)
	// ParseWebMessage may replace the edit protocol envelope with its body.
	// Recover the target key and revision from RawMessage in that case.
	if raw := unwrap(evt.RawMessage); raw.GetProtocolMessage() != nil {
		content = raw
	}
	if evt.SourceWebMsg != nil {
		stub := evt.SourceWebMsg.GetMessageStubType()
		if stub == waWeb.WebMessageInfo_REVOKE || stub == waWeb.WebMessageInfo_ADMIN_REVOKE {
			m.Deleted = true
			m.Kind = "excluída"
			return m, true, nil
		}
		if stub != waWeb.WebMessageInfo_UNKNOWN && content == nil {
			m.Kind = "evento (" + stub.String() + ")"
			return m, true, nil
		}
	}
	if content == nil {
		return m, true, nil
	}
	if p := content.GetProtocolMessage(); p != nil {
		if p.GetType() != waE2E.ProtocolMessage_REVOKE && p.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT {
			return m, false, nil
		}
		key := p.GetKey()
		if key == nil || key.GetID() == "" {
			return m, false, fmt.Errorf("atualização sem chave da mensagem")
		}
		m.ID = key.GetID()
		m.FromMe = key.GetFromMe()
		m.Mutation = true
		if !info.Timestamp.IsZero() {
			m.Version = info.Timestamp.UnixMilli()
		}
		if p.GetTimestampMS() > 0 {
			m.Version = p.GetTimestampMS()
			m.Evidence.EventTime = millisTime(p.GetTimestampMS())
		}
		if key.GetRemoteJID() != "" {
			j, e := types.ParseJID(key.GetRemoteJID())
			if e != nil {
				return m, false, e
			}
			m.Chat = jid(j)
			m.Group = j.Server == types.GroupServer
		}
		if key.GetParticipant() != "" {
			j, e := types.ParseJID(key.GetParticipant())
			if e != nil {
				return m, false, e
			}
			m.Sender = jid(j)
		}
		m.Wire = nil
		if p.GetType() == waE2E.ProtocolMessage_REVOKE {
			m.Deleted = true
			m.Kind = "excluída"
			return m, true, nil
		}
		m.Edited = true
		content = unwrap(p.GetEditedMessage())
	}
	if content == nil {
		return m, true, nil
	}
	var context *waE2E.ContextInfo
	switch {
	case content.Conversation != nil:
		m.Kind = "texto"
		m.Text = content.GetConversation()
	case content.GetExtendedTextMessage() != nil:
		p := content.GetExtendedTextMessage()
		m.Kind = "texto"
		m.Text = p.GetText()
		context = p.GetContextInfo()
	case content.GetReactionMessage() != nil:
		p := content.GetReactionMessage()
		m.Kind = "reação"
		m.Text = p.GetText()
		m.Evidence.Reaction = &archive.ReactionObservation{EventMessageID: info.ID, ActorID: jid(info.Sender), ActorName: optionalString(info.PushName), Emoji: p.Text, Time: millisTime(p.GetSenderTimestampMS())}
		if m.Text == "" {
			m.Text = "Reação removida"
		}
		reactionReference(&m, p.GetKey())
	case content.GetEncReactionMessage() != nil:
		m.Kind = "reação"
		m.Text = "Reação indisponível"
		m.Evidence.Reaction = &archive.ReactionObservation{EventMessageID: info.ID, ActorID: jid(info.Sender), ActorName: optionalString(info.PushName)}
		reactionReference(&m, content.GetEncReactionMessage().GetTargetMessageKey())
	case content.GetImageMessage() != nil:
		p := content.GetImageMessage()
		m.Evidence.Caption = p.Caption
		m.Evidence.Attachments = []archive.Attachment{{ID: info.ID + ":0", MIME: optionalString(p.GetMimetype()), Size: p.FileLength, SHA256: optionalHash(p.GetFileSHA256()), Availability: "not_retained"}}
		m.Kind = "imagem"
		m.Text = p.GetCaption()
		context = p.GetContextInfo()
	case content.GetVideoMessage() != nil:
		p := content.GetVideoMessage()
		m.Evidence.Caption = p.Caption
		m.Evidence.Attachments = []archive.Attachment{{ID: info.ID + ":0", MIME: optionalString(p.GetMimetype()), Size: p.FileLength, SHA256: optionalHash(p.GetFileSHA256()), Availability: "not_retained"}}
		m.Kind = "vídeo"
		m.Text = p.GetCaption()
		context = p.GetContextInfo()
	case content.GetAudioMessage() != nil:
		p := content.GetAudioMessage()
		m.Evidence.Attachments = []archive.Attachment{{ID: info.ID + ":0", MIME: optionalString(p.GetMimetype()), Size: p.FileLength, SHA256: optionalHash(p.GetFileSHA256()), Availability: "not_retained"}}
		m.Kind = "áudio"
		descriptor := &waE2E.AudioMessage{URL: p.URL, Mimetype: p.Mimetype, FileSHA256: p.FileSHA256, FileLength: p.FileLength, Seconds: p.Seconds, PTT: p.PTT, MediaKey: p.MediaKey, FileEncSHA256: p.FileEncSHA256, DirectPath: p.DirectPath}
		if p.GetDirectPath() != "" && len(p.GetMediaKey()) > 0 {
			m.AudioMetadata, err = proto.Marshal(descriptor)
			if err != nil {
				return m, false, err
			}
		}
		context = p.GetContextInfo()
	case content.GetDocumentMessage() != nil:
		p := content.GetDocumentMessage()
		m.Evidence.Caption = p.Caption
		m.Evidence.Attachments = []archive.Attachment{{ID: info.ID + ":0", MIME: optionalString(p.GetMimetype()), Size: p.FileLength, SHA256: optionalHash(p.GetFileSHA256()), Availability: "not_retained"}}
		m.Kind = "documento"
		m.Text = p.GetCaption()
		m.FileName = p.GetFileName()
		m.Evidence.Attachments[0].Name = optionalString(p.GetFileName())
		context = p.GetContextInfo()
	case content.GetStickerMessage() != nil:
		m.Kind = "figurinha"
		context = content.GetStickerMessage().GetContextInfo()
	case content.GetLocationMessage() != nil:
		p := content.GetLocationMessage()
		m.Kind = "localização"
		m.Text = fmt.Sprintf("%s %s (%f, %f)", p.GetName(), p.GetAddress(), p.GetDegreesLatitude(), p.GetDegreesLongitude())
		context = p.GetContextInfo()
	case content.GetContactMessage() != nil:
		p := content.GetContactMessage()
		m.Kind = "contato"
		m.Text = p.GetDisplayName()
		context = p.GetContextInfo()
	default:
		var fields []string
		content.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			if strings.HasSuffix(string(fd.Name()), "Message") {
				fields = append(fields, string(fd.Name()))
			}
			return true
		})
		sort.Strings(fields)
		m.Kind = "não interpretado"
		if len(fields) > 0 {
			m.Kind += " (" + strings.Join(fields, ", ") + ")"
		}
	}
	if context != nil {
		m.ReplyID = context.GetStanzaID()
		m.ReplySender = context.GetParticipant()
		m.Evidence.Mentions = context.GetMentionedJID()
		m.Evidence.Forwarded = context.IsForwarded
		m.Evidence.ForwardingScore = context.ForwardingScore
		if q := unwrap(context.GetQuotedMessage()); q != nil {
			switch {
			case q.Conversation != nil:
				m.Evidence.QuotedText = q.Conversation
			case q.GetExtendedTextMessage() != nil:
				m.Evidence.QuotedText = q.GetExtendedTextMessage().Text
			case q.GetImageMessage() != nil:
				m.Evidence.QuotedText = q.GetImageMessage().Caption
			case q.GetVideoMessage() != nil:
				m.Evidence.QuotedText = q.GetVideoMessage().Caption
			case q.GetDocumentMessage() != nil:
				m.Evidence.QuotedText = q.GetDocumentMessage().Caption
			}
		}
	}

	if m.Time.IsZero() {
		m.Time = time.Unix(0, 0).UTC()
	} // Legacy renderers use epoch; the event contract exports null.
	return m, true, nil
}

// A reaction has its own message key. Its target is a reference, never an edit.
func reactionReference(m *archive.Message, key *waCommon.MessageKey) {
	if key == nil {
		return
	}
	if remote := key.GetRemoteJID(); remote != "" {
		chat, err := types.ParseJID(remote)
		if err != nil || jid(chat) != m.Chat {
			return
		}
	}
	m.ReplyID = key.GetID()
	if m.Evidence != nil && m.Evidence.Reaction != nil {
		m.Evidence.Reaction.TargetID = key.GetID()
		m.Evidence.Reaction.TargetSender = key.GetParticipant()
		m.Evidence.Reaction.TargetFromMe = key.GetFromMe()
	}
	if sender, err := types.ParseJID(key.GetParticipant()); err == nil {
		m.ReplySender = jid(sender)
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func optionalHash(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := hex.EncodeToString(b)
	return &s
}
func millisTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

func sourceTime(t time.Time) *time.Time {
	if t.IsZero() || t.Equal(time.Unix(0, 0)) {
		return nil
	}
	v := t.UTC()
	return &v
}
