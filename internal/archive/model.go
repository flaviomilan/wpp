package archive

import "time"

// Message is independent of the WhatsApp transport. Wire contains only the
// MessageInfo necessary to request history, never session keys or media keys.
type Message struct {
	Evidence    *Observation `json:",omitempty"`
	Chat        string
	ID          string
	Sender      string
	Author      string
	FromMe      bool
	Group       bool
	Time        time.Time
	Kind        string
	Text        string
	FileName    string
	ReplyID     string
	ReplySender string
	Edited      bool
	Deleted     bool
	Mutation    bool
	Version     int64
	Wire        []byte
	// AudioMetadata is a private download descriptor, stored only in audio_jobs.
	AudioMetadata []byte      `json:"-"`
	Transcript    *Transcript `json:",omitempty"`
}

type TranscriptSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}
type Transcript struct {
	ObservedAt  *time.Time `json:",omitempty"`
	Origin      string     `json:",omitempty"`
	Text        string
	Language    string
	Model       string
	ModelSHA256 string
	Engine      string
	Created     time.Time
	Segments    []TranscriptSegment
}

type Chat struct {
	ID          string
	Name        string
	Tracked     bool
	File        string
	Generation  int64
	Exported    int64
	Count       int
	First, Last time.Time
	Seed        *Message
}

type Attempt struct {
	ID                int64
	Chat              string
	Started, Finished time.Time
	Anchor            string
	AnchorKey         string
	State             string
	Detail            string
	BoundaryObserved  bool
}

type BatchResult struct {
	Inserted int
	Overlap  int
	Oldest   *Message
}

// Observation contains only normalized public evidence; download secrets stay in audio_jobs.
type Observation struct {
	EventTime             *time.Time            `json:"event_time"`
	EventMessageID        string                `json:"event_message_id"`
	ReactionsAvailable    bool                  `json:"reactions_available"`
	ReactionsCompleteness string                `json:"reactions_completeness"`
	ObservedAt            *time.Time            `json:"observed_at"`
	Origin                string                `json:"origin"`
	NativeSender          string                `json:"native_sender"`
	SenderAliases         []string              `json:"sender_aliases"`
	GroupName             string                `json:"group_name"`
	Caption               *string               `json:"caption"`
	QuotedText            *string               `json:"quoted_text"`
	Mentions              []string              `json:"mentions"`
	Forwarded             *bool                 `json:"forwarded"`
	ForwardingScore       *uint32               `json:"forwarding_score"`
	Attachments           []Attachment          `json:"attachments"`
	Reaction              *ReactionObservation  `json:"reaction"`
	Reactions             []ReactionObservation `json:"reactions"`
}
type Attachment struct {
	ID           string  `json:"attachment_id"`
	Name         *string `json:"file_name"`
	MIME         *string `json:"mime_type"`
	Size         *uint64 `json:"size_bytes"`
	SHA256       *string `json:"sha256"`
	Reference    *string `json:"file_reference"`
	Availability string  `json:"availability"`
}
type ReactionObservation struct {
	ActorIsAccount bool       `json:"actor_is_account"`
	EventMessageID string     `json:"event_message_id"`
	ActorID        string     `json:"actor_id"`
	ActorName      *string    `json:"actor_name"`
	TargetID       string     `json:"target_id"`
	TargetSender   string     `json:"target_sender"`
	TargetFromMe   bool       `json:"target_from_me"`
	Emoji          *string    `json:"emoji"`
	Time           *time.Time `json:"occurred_at"`
}
