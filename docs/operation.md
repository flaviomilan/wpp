# Operation and command reference

wpp uses SQLite as its primary archive and JSONL as its data export format. See [Capture behavior](how-it-works.md), [Web interface](interface.md), and the [JSONL contract](jsonl-events.md).

## Installation and first use

Build with Go 1.26, toolchain 1.26.9, and a C compiler for SQLite. Operational support uses `flock` on Linux/macOS; Windows is unsupported. Building and running do not require Node.js. Development checks use Node.js 24+ and npm.

```bash
make build
./wpp serve
```

In Settings, link the account using the QR code, discover chats, and track the ones you want. Choose continuous capture or a one-off sync. Closing the tab keeps the process running; Ctrl+C stops it and tries to complete pending exports. A new installation starts paused and remembers preferences.

CLI alternative:

```bash
./wpp login
./wpp chats --groups
./wpp track '123456789@g.us'
./wpp run
```

Copy the full IDs shown in the listing. Example identifiers are fictitious. `run` stays in the foreground; an external supervisor can restart it and send SIGTERM. Never run two capture owners against one data directory.

## Application language

English is the default when no preference exists. Browser, operating-system, and terminal locales do not select a language. Select **Settings > Advanced > Application language**, choose **English** or **Português (Brasil)**, and use **Save language**. This works without an audio model, linked account, or capture ownership.

```bash
./wpp language             # Show the saved language, or en when unset
./wpp language pt-BR       # Save Brazilian Portuguese for this data directory
./wpp --language en status # Override presentation for this invocation only
./wpp language en          # Save English
```

Web and CLI share the saved choice in the selected data directory. Other directories have their own preference. An already-running CLI keeps its invocation language. With `serve`, `--language` also supplies the panel's initial language; explicitly saving a language in the panel replaces that initial override in the browser session. Overrides never save a preference implicitly. Saving can fail if storage is unavailable; the panel retains the selection for retry and does not announce success.

Application language does not change audio recognition language (`--audio-language`, default `pt`), the configured time zone, archived messages, names, transcripts, or JSONL data. Developer tool diagnostics and documentation are in English. Do not parse localized CLI prose as a machine contract.

## Command reference

Syntax: `wpp [global options] command [arguments]`.

| Command | Result |
|---|---|
| `language [en\|pt-BR]` | Read or save the application language |
| `login` | Link via QR; reuse an existing session |
| `chats [--groups]` | Discover and list chats with full IDs |
| `track <id>` / `untrack <id>` | Enable/disable tracking while preserving records |
| `run` | Capture, retrieve history, process audio, and update JSONL |
| `serve` | Web workspace and controller; consultation mode if the lock is occupied |
| `status` | Observed coverage and recovery attempts |
| `export` | Rebuild complete `chat-<hash>.jsonl` files |
| `events next <group>` | Publish or restore the pending JSONL batch |
| `events ack <group> <batch>` | Acknowledge after the consumer commits |
| `events status <group>` | Acknowledged cursor, pending batch, remaining events |
| `batch next\|ack\|status ...` | Alias for the same `events` protocol |
| `evidence <group> <source>` | Publish a source's event history as JSONL |
| `audio status` | Audio queue summary |
| `audio retry <source>` | Requeue a failure with available private download metadata |
| `audio correct <source> <file.json>` | Record a versioned transcript correction |

`status`, language selection, tracking, exports, batches, evidence, and corrections do not connect to WhatsApp. Commands opening SQLite can run migrations. Batches and evidence accept `@g.us` groups; complete exports cover archived chats. Exporting existing messages requires a linked account.

## Options and directories

| Option | Default / purpose |
|---|---|
| `--data-dir` | `./data`: database, session, locks, temporary workspace |
| `--export-dir` | `./exports`: complete files, batches, evidence |
| `--language` | Invocation override: `en` or `pt-BR` |
| `--timezone` | `America/Sao_Paulo`: panel/status display; events use RFC 3339 UTC |
| `--listen` | `127.0.0.1:8080`: local panel address |
| `--whisper-model` | Local model path; enables transcription in `run` |
| `--whisper-bin` / `--ffmpeg-bin` | Recognition/conversion executables |
| `--audio-language` | `pt`; `auto` enables detection |
| `--audio-vocabulary` | Supporting vocabulary, without translation |
| `--audio-threads` | `0`: select up to 8 CPUs automatically |
| `--audio-timeout` | `30m`: time limit per audio |

```bash
./wpp --data-dir ./data --export-dir ./exports run
./wpp --data-dir ./data --export-dir ./exports export
```

Relative paths start at the working directory. Use matching directories for capture and consumption. The old `--batch-max-bytes`, `--batch-max-messages`, `--context-max-bytes`, `--references-max-bytes`, and `--evidence-revision` options were removed with Markdown export. JSONL batches preserve whole events: up to 100 per batch, targeting 1 MiB; an oversized event travels alone.

## Files and responsibilities

```text
data/
  archive.db             # messages, transcripts, events, reservations, cursors
  session.db             # private WhatsApp session
  tmp-audio/             # temporary processing files
exports/
  chat-<hash>.jsonl       # complete event history, rebuilt automatically
  events/chat-<hash>/
    E000001/events.jsonl  # immutable batch
    E000001/manifest.json # version, cursors, account, group, count, hash
    evidence/source-<hash>.jsonl
```

JSON manifests are control metadata. Exported records use the same versioned JSONL contract. wpp does not generate context, people directories, batch inputs, or demand records in Markdown.

Complete files update after two seconds without changes. A continuously active chat can wait until a pause; graceful shutdown forces an update attempt. Files contain event history, including versions before edits/deletions, rather than only current message state. Do not edit generated files: they will be rebuilt.

## Consumption and concurrency

```bash
./wpp events next '123456789@g.us'
# Check the manifest; commit in the consumer with uniqueness on event_id.
./wpp events ack '123456789@g.us' E000001
./wpp events status '123456789@g.us'
```

Each group has one cursor and one pending batch. `events` and `batch` share their lock, cursor, and files; they are not separate consumers. Until acknowledgement, `next` returns the same bytes. The manifest is published last; `ack` verifies integrity. Complete files and evidence do not advance this cursor.

`run` and the web controller hold an exclusive capture lock. Batch/evidence reads can coexist with capture writes: SQLite provides transactional consistency and complete export uses a separate WAL reader. Closing an HTTP request does not cancel accepted operations; cancellation is explicit.

## Updates, backup, and restore

Stop the owner before replacing the executable or copying databases. Back up the entire data directory and published batches, retaining permissions. Copying only `archive.db` during writes can miss WAL data; stop cleanly or use a consistent SQLite backup.

Migration to schema 7 preserves messages, transcripts, sources, event IDs, and JSONL cursors. It changes complete export names from `.md` to `.jsonl` and invalidates the old export marker for rebuilding. Older binaries reject the upgraded database; there is no automatic downgrade.

Legacy Markdown files and reservations remain, without regeneration or automatic deletion. Old Markdown acknowledgements do not acknowledge JSONL. Reprocess JSONL with `event_id` deduplication. Restore consistent data and export backups together; never mix accounts. Retain the database to preserve internal IDs and consumer reservations. Application language adds an independent metadata key without changing the database schema version.

## Output, failures, and limits

Failures return a nonzero CLI exit code, including export failures during shutdown. JSONL failure does not undo stored messages. Correct paths/permissions and run `export`; restore an incomplete batch with `events next` before acknowledgement.

`ack` declares consumption, not semantic correctness or WhatsApp completeness. Reconnection, unavailable history, encrypted content, and unobserved edits/deletions remain limitations. Missing messages or emojis are not business facts.

Audio is temporary; transcripts and provenance persist. Download keys are discarded after success. See [Transcription](transcription.md) and [JSONL events](jsonl-events.md) for processing and corrections. Sessions and private download descriptors are never exported. Historical evidence can retain content preceding deletion, which affects retention and access policies. Synthetic tests do not establish coverage for a real account.

## Validation with a real account

Use a test chat with participants' consent. Check text, replies, mentions, media, audio, added/changed/removed reactions, and edits. Test disconnection, reconnection, and recovery against the phone without assuming complete history. Validate batches and repeat a delivery to confirm consumer deduplication.

## Background capture on Linux

wpp stays in the foreground and does not daemonize itself. An external supervisor can run `run` without a browser. [wpp-capture.service](../ops/wpp-capture.service) is an adaptable systemd **user** unit, not automatically installed. Installing it for the current account does not require root.

```bash
install -Dm755 ./wpp "$HOME/.local/bin/wpp"
mkdir -p "$HOME/.local/share/wpp" "$HOME/.config/systemd/user"
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" login
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" chats
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" track 'FULL_ID_SHOWN'
install -m644 ops/wpp-capture.service "$HOME/.config/systemd/user/wpp-capture.service"
systemctl --user daemon-reload
systemctl --user enable --now wpp-capture.service
```

Link the account and track at least one chat before starting. The example uses `%h/.local/share/wpp` as its working directory and explicit data/export paths. `%h` is expanded by systemd, not the shell. Never copy another account's databases into it. For existing data, stop its owner, back up, and adapt the unit's paths.

```bash
systemctl --user status wpp-capture.service
journalctl --user -u wpp-capture.service -f
systemctl --user stop wpp-capture.service
systemctl --user restart wpp-capture.service
systemctl --user disable --now wpp-capture.service
```

Stopping sends SIGTERM and waits up to 60 seconds. The application cancels operations, finishes accepted writes, and tries pending exports. A forced kill after timeout can leave exports behind; the next run rebuilds from SQLite. `Restart=on-failure` restarts failures, not explicit stops. Logs stay in the journal; share sanitized excerpts only.

For transcription, install Whisper/model/FFmpeg using [the transcription guide](transcription.md) and add global options with absolute paths before `run` in `ExecStart`. Audio is optional; no author-specific model path is required. Changes to the unit require `daemon-reload` and restart. Language preferences use the service's data directory; add `--language en` before `run` only to force a temporary service presentation override.

A user service follows the user's session. An operator can enable lingering with `loginctl enable-linger "$USER"` if machine policy allows it; the project never does this automatically. Review disk/CPU use and backup policy before enabling startup.

### Using the panel with a service

With `run` active, `wpp --data-dir ... serve` opens in consultation mode and cannot control that process. Stop the service and run `serve` with matching paths for web control. Alternatively supervise `serve` instead of `run`, keeping its address on loopback. Never force ownership by deleting a lock. Closing the tab keeps `serve` alive; stopping its process stops processing. Status reads, people registration, eligible audio requeue, and application language selection retain their documented capabilities.

### macOS

Foreground `run` and `serve` use the same commands. The systemd example is Linux-specific. On macOS, configure a user `launchd` agent with absolute paths, equivalent signals, and restart policy. This version does not provide or install a launchd unit. Windows is unsupported.
