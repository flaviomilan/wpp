# JSONL events for external consumers

wpp supplies evidence without interpreting decisions, purchases, payments, or emoji meaning. Groups retain native IDs; associating a group with a wedding belongs to the consumer. There is no LangChain/LangGraph dependency.

The public contract is [events-1.0.schema.json](schemas/events-1.0.schema.json), JSON Schema Draft 2020-12. [Synthetic examples](examples/events-1.0.jsonl) contain one complete UTF-8 JSON object per line. Internal newlines are escaped; events are never fragmented. Examples contain no real data.

## Prepare, consume, and acknowledge

```bash
./wpp events next '123456789@g.us'
# Returns exports/events/chat-<hash>/E000001/events.jsonl
./wpp events status '123456789@g.us'
./wpp events ack '123456789@g.us' E000001
```

Global `--data-dir`/`--export-dir` options precede `events`. These commands are offline SQLite operations: they neither request history nor connect to WhatsApp. The directory must have a linked account. Capture continues through `run`, the panel, and configured tracking.

`wpp export` and panel export actions generate complete event history in `exports/chat-<hash>.jsonl`; capture maintains it automatically. `wpp evidence <group> <source>` publishes that source's events as JSONL. All use the same contract without acknowledging consumption. There is no Markdown generator; legacy files remain. Renaming exports invalidates old export generations without changing messages, transcripts, or events.

1. Run `next`; it reports when no new events exist.
2. Read adjacent `manifest.json`, published last. Check SHA-256 of the exact JSONL bytes, count, account, group, and version.
3. Within the consumer transaction, insert events uniquely by `event_id`, update projections, and retain raw events for reprocessing.
4. Commit the consumer transaction before calling `ack`.
5. Repeat. A crash between commit and `ack` causes redelivery; uniqueness prevents duplication.

A pending batch is an immutable database reservation. New messages never change its bytes. Repeating `next` restores original JSONL and manifest. `ack` rejects files differing from the reservation and is idempotent. Acknowledged files remain on disk for reprocessing. To rebuild a consumer, reread retained batches and continue with `next`; never delete the source database.

Each group has one cursor and one pending batch. `batch` aliases `events`, sharing files and acknowledgements. Complete export and evidence never advance this cursor. No context/demand memory files are generated. Batches hold up to 100 events and target 1 MiB; an oversized event goes whole and alone. This bounds batches without truncating content.

## Envelope, identity, and time

| Field | Meaning |
|---|---|
| `schema_version` | String `1.0`, independent of legacy Markdown manifest versions |
| `event_id` | Stable internal occurrence ID; deduplicate by it |
| `sequence` | Local persistence order, potentially with gaps between a group's events |
| `event_type` | Observed event or enrichment type |
| `source` | Always `whatsapp` |
| `account_id` | Linked account JID without a device component |
| `group_id` | Native group ID |
| `message_id` | Native message ID; target message ID for reactions |
| `occurred_at` | Source occurrence timestamp or `null` |
| `observed_at` | First persisted observation or `null` for legacy data |
| `provenance` | Origin, derivation flag, explicit limitations |
| `payload` | Schema-defined event data |

Timestamps are RFC 3339 UTC with `Z`. Export/migration time never substitutes for source time. Legacy renderers' absent-date epoch becomes `null`.

`message_id` alone is not globally unique. Use `(account_id, group_id, native_key.message_id, native_key.sender_id, native_key.from_me)`. Reactions use the equivalent `target_message` key. The reaction actor is not the target author; `reaction_message_id` identifies the native reaction occurrence.

Internal `source_id` identifies an archived source; `content_version` identifies its revision. Neither is a WhatsApp ID. Event IDs persist across re-export of the same archive. Preserve/restore SQLite backups: rebuilding a separate archive can produce different internal IDs.

`participant_id` is locally canonicalized only with proven equivalence. `native_id` and `aliases` retain observed IDs. Never merge people by names or ID similarity alone. Later proven aliases can require projection reconciliation by native ID; old events are not rewritten.

Names reflect what was available at observation, not current registration at export. A group name learned later is not retroactively treated as known in an old message. Unknown names/fields remain null. Application language never changes event data or enum values.

## Content and transcription

Messages separate `text`, `caption`, `reply.quoted_text`, mentions, forwarding, and attachments. Text remains original rather than summarized. Public kinds are `text`, `audio`, `image`, `video`, `document`, and `other`; `original_type` retains the collector subtype. This integration performs no OCR.

`attachments: null` means unavailable metadata in legacy records; `[]` means the normalizer knows no attachments for this content. Public metadata includes name, MIME, size, and source SHA-256. `file_reference` is null without a persistent file. Images, videos, and documents are not newly downloaded.

Audio and working files remain temporary. `attachment_id` plus available attachment metadata/hash identify them. `file_reference: null` does not hide a persistent sound file; it cannot reopen audio.

On Whisper completion, `message.transcribed` supplies text, language, model/hash, engine/version, `processed_at`, millisecond segments, and `transcription_version`. Original message text is unchanged. `occurred_at` is null because transcription is local processing; processing and observation times are separate. Acoustic accuracy depends on recording, model, and vocabulary; the contract does not certify recognized speech.

For an operator correction:

```bash
./wpp audio correct 'S-<audio-source>' transcript.json
```

The file contains one JSON object, at most 16 MiB:

```json
{
  "text": "The tasting will be on Sunday at ten.",
  "language": "en",
  "model": "ggml-large-v3-turbo.bin",
  "model_sha256": "model-hash-when-known",
  "engine": "manual-correction",
  "processed_at": "2026-10-15T18:40:00Z",
  "segments": [{"start_ms": 0, "end_ms": 2800, "text": "The tasting will be on Sunday at ten."}]
}
```

Text must be nonempty and at most 4 MiB; segment times must be valid. Omit unknown metadata and do not attribute work to a model that did not perform it. Audio must already have a transcript and must not be deleted. Corrections create another version, preserve the previous one, and set provenance `correction`. Identical content/metadata does not create another version; changing processing time declares a distinct processing occurrence. `audio retry` remains for failures with available private descriptors. Successful processing discards download keys; another automatic run is not guaranteed without renewed original access.

## Reactions and late links

`reaction.added`, `reaction.updated`, and `reaction.removed` preserve actor, observed name, full Unicode emoji, native occurrence, and target key. Explicit removal uses `emoji: ""`; unknown emoji uses `null`. The legacy display string “Reação removida” is not an emoji.

If the reaction lacks its own timestamp but its envelope has occurrence time, `occurred_at` uses that time and records `reaction_time_from_envelope`. Snapshots never use target message time as reaction time.

A first positive reaction is an observed addition. `previous_state_known: false` does not prove no earlier reaction existed. An update inferred by comparison with an earlier individual occurrence is marked `derived: true`; comparison requires orderable timestamps. Otherwise previous state is not asserted. Older occurrences cannot regress newer state.

`target_message` is a frozen copy of locally available content at observation: text, caption, transcript, and their versions. It supplements rather than replaces the original event. Later edits/corrections never alter it.

Absent targets use `content_status: "unavailable"`, null content/versions, and available native keys. Link later arrivals using account/group/target key. Without an author, resolve only a unique match; `ambiguous` retains uncertainty. For current audio state, read the latest source transcript without changing reaction snapshots.

History lists produce `reaction.snapshot`: `reactions` retains available participants, emojis, and times without reconstructing earlier actions. `completeness` can be `complete`, `partial`, or `unknown`; current integration normally supports only `unknown`. Lists are never compared to derive transitions.

`reactions: []` with known availability means a known empty list. `reactions: null` with `availability: "unavailable"` means unavailable. An empty repeated protobuf field does not establish an empty list: the message records `reactions_availability: "unavailable"` instead of inventing an empty snapshot. Encrypted/legacy reactions without content produce unavailable snapshots.

## Actual support, migration, and limits

| Event | Source / limitation |
|---|---|
| `message.created` | First locally available version, including imported records |
| `message.edited` / `message.deleted` | Observed edit/revocation protocols; not proof every change was observed |
| `message.enriched` | Recovered content/metadata without proven edit |
| `message.transcribed` | Local completion or explicit correction; previous versions retained |
| `reaction.added` / `updated` / `removed` | Available individual reactions; inferred updates are flagged |
| `reaction.snapshot` | History lists or unavailable reaction content |

Pinned whatsmeow exposes `ReactionMessage` with key/text/optional timestamp and historical `WebMessageInfo.reactions`. wpp consumes them and attempts local `DecryptReaction` when the session has the message secret. Failures preserve unavailability without exposing secrets. This certifies neither list completeness, complete history, nor encrypted reaction availability. Protocol definitions: [waE2E](https://github.com/tulir/whatsmeow/blob/4cf8bc8e0aeb/proto/waE2E/WAWebProtobufsE2E.proto), [waWeb](https://github.com/tulir/whatsmeow/blob/4cf8bc8e0aeb/proto/waWeb/WAWebProtobufsWeb.proto).

Additive migration raises the database to version 7, preventing older binaries from silently writing an upgraded archive. Back up before upgrading; no automatic downgrade exists. Existing `changes` revisions are imported once per source/revision. Legacy records without observation use `observed_at: null`. Previously discarded historical names, mentions, exact reaction times, or content available at old reaction time cannot be recovered retrospectively. Missing historical snapshots stay unavailable rather than being filled with current text.

An explicit edit/deletion preceding the original produces its event with unknown original send time; later arrival enriches the link. Already-edited/deleted imported records can begin with `message.created` plus flags/limitations, without claiming creation and mutation were separately observed. `provenance.origin` distinguishes `live`, `history`, `recovered`, `migration`, `processing`, and `correction`.

Delivery follows local order, including reconnection/recovery. `ack` acknowledges consumption, not WhatsApp completeness. Deletions never erase earlier revisions. Apply later versions for current projections and retain earlier versions for audit as needed.

JSONL uses an explicit public field allowlist. It excludes sessions, media keys, private descriptors, binary/base64 content, and temporary download URLs. Business interpretation and evidence retention remain the consumer's responsibility.
