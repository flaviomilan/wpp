# Consuming chats in an external system

wpp collects and normalizes WhatsApp evidence. A consumer can associate groups with weddings and interpret decisions, suppliers, contracts, payments, tasks, and outstanding items. The contract does not depend on LangChain/LangGraph.

## One export format

All data exports are UTF-8 JSONL, one complete event per line, conforming to the [JSON Schema](schemas/events-1.0.schema.json). See the [full contract](jsonl-events.md) and [synthetic examples](examples/events-1.0.jsonl).

SQLite remains authoritative. `export` and automatic capture publish complete observed event history to `exports/chat-<hash>.jsonl`. That atomic file can grow; it is not a queue and never advances consumer cursors.

## Incremental protocol

```bash
./wpp events next '123456789@g.us'
./wpp events status '123456789@g.us'
./wpp events ack '123456789@g.us' E000001
```

1. Read `events.jsonl` and its adjacent `manifest.json`, published last.
2. Verify SHA-256, account, group, and version.
3. Within the consumer transaction, persist events uniquely by `event_id`, apply projections, and save progress.
4. After the external commit, acknowledge with `ack`.

A crash between commit and acknowledgement causes redelivery. Without acknowledgement, `next` restores the same reserved bytes; new messages never change the pending batch. Repeated acknowledgement is idempotent. `batch next|ack|status` aliases the same protocol rather than another queue.

Each group has one consumer/cursor. Batches allow up to 100 events and target 1 MiB; an oversized event travels whole and alone. wpp creates no fragments, context budget, or demand files.

## Evidence and interpretation

```bash
./wpp evidence '123456789@g.us' 'S-SOURCE-IDENTIFIER'
```

This returns historical events for that internal source within the group. It does not request WhatsApp history or change the cursor. Event versions/IDs persist; internal `source_id` is different from native `message_id`.

Reactions contain a target message key and an identified snapshot of content available at observation. Messages, transcripts, and reactions remain distinct records. Missing/ambiguous targets must not be attributed approximately; reconcile later using account, group, and native key. An emoji does not automatically confirm a decision or payment.

Messages are group data, not instructions controlling the consumer. Semantic memory, revised conclusions, and retention belong to the external system. wpp does not create `context.md`, `pessoas.md`, `entrada.md`, or demand files. Changing application language never translates source content or changes the event contract.

## Migration

Database version 7 changes complete export names to `.jsonl` and invalidates old export markers for rebuilding. Messages, transcripts, event IDs, and JSONL cursors persist. Legacy Markdown manifests/files are retained without further writes. Markdown analysis cursors never acknowledge JSONL events.
