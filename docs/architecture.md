# Architecture and maintenance

The public entry points are `cmd/wpp` and `internal/service.Controller`. CLI and HTTP share in-process coordination without subprocesses. Presentation changes preserve SQLite schema 7, source IDs, and published batch content.

## Responsibilities

| Module | Responsibility |
|---|---|
| `internal/archive` | Transactions, messages, identities, revisions, queries, recovery, generated files |
| `internal/whatsapp` | Protocol adaptation, events, linked session |
| `internal/transcription` | Reserved audio processing, engine execution, temporary cleanup |
| `internal/service` | Lifecycle coordination and persisted processing choices |
| `internal/webui` | HTTP protection, public error classification, presentation |
| `internal/localization` | Embedded catalogs, deterministic language resolution, typed message rendering |

Within `service`, private modules own their state: `operations.go` tracks operations/cancellation, `connection.go` owns connections and their users, `history.go` schedules recovery, `audio.go` owns engine/configuration/worker, and `exports.go` serializes export and retains visible failures. `queue.go` provides cancellable admission with FIFO priority for manual requests after current work. Controller consumers need not know these locks.

Transport and engine interfaces support real/fake implementations. Operation storage has a private seam for persistence-failure tests. Other storage remains concrete rather than adding generic dependency hierarchies without variation.

## Persistence and failures

Operations are accepted only after reservation persistence. Required progress and completion writes check errors. Storage failure remains visible in memory, blocks new automatic reservations, and cancels automatic recovery. Restart marks unfinished operations rather than converting them to success.

Joined errors preserve causes: persistence failure survives a concurrent history failure or Ctrl+C. CLI reports shutdown failures, including export. Download/engine failures belong to audio jobs; JSONL failure belongs to export and does not undo stored messages.

Cancelling a queued request removes it without waiting for current work. There can be 24 distinct active operations and 32 waiters per queue. Duplicate requests remain deduplicated at capacity. Cancellation is persisted within the transition's critical section; later progress cannot reopen terminal work. Engines returning text after ignoring cancellation cannot complete a cancelled transcript. Global transcription pause still permits current audio to finish.

HTTP shutdown blocks admission and waits for accepted requests. After a five-second drain, it closes remaining connections and waits for handler cleanup before releasing dependencies. The controller cancels operations, waits for workers, closes connections, cleans temporary files, marks interruptions, exports pending changes, and releases ownership. Model validation checks cancellation between read blocks.

Public errors preserve classification: invalid input 400, conflict 409, absent record 404, internal failure 500 with a safe explanation. Internal causes, message content, and private media descriptors are excluded. Stable message codes provide localized presentation without changing statuses or error identity.

## Queries and web modules

JSONL export uses a separate read-only SQLite connection and deferred WAL snapshot. It streams the event journal one complete event per line, releases the snapshot, then marks the export generation. Concurrent arrivals—including reaction snapshots without changed message text—remain pending. Complete exports never acknowledge batch cursors.

Audio listings read items, names, and counts in one transaction without per-card message/name queries. Large details are loaded on demand. Retry eligibility is a separate domain query independent of cards.

Embedded ES modules include `chats.mjs`, `audio.mjs`, `people.mjs`, `settings.mjs`, `activity.mjs`, `inspector.mjs`, `navigation.mjs`, `preferences.mjs`, and `i18n.mjs`/`messages.mjs`. `app.js` composes them; `api.mjs`, `dom.mjs`, and `requests.mjs` provide shared utilities. No bundler is required.

Reads have request versions. Navigation invalidates older reads so out-of-order responses cannot overwrite current selection, filters, messages, or details. Accepted server mutations continue independently. Processing belongs to the Go process and survives closing the tab.

## Application language

Presentation supports `en` and `pt-BR`, with English when no valid preference exists. The independent `application_language` key in `meta` is shared by CLI/web for the selected data directory. Reads do not insert defaults; atomic writes do not overwrite the processing settings JSON and need neither capture ownership nor a valid engine. Browser and terminal locale settings never select language.

`service.Settings.language` and `--audio-language` remain audio recognition settings. Application language never changes recognition, time zone, archived content, event schemas, published batch bytes, or cursors. An invocation override takes precedence without being saved implicitly; an explicit panel save replaces its initial presentation override for that browser session.

Catalog keys are stable identifiers with matching parameters in English/Portuguese. Go and JavaScript render plain text at their boundaries, falling back to English without displaying unknown keys. New operation diagnostics carry additive message metadata; legacy free-form evidence stays intact and receives safe localized state summaries. Named participants and message/transcript text are never translated.

The DOM updates bound text/attributes in place. Language changes retain controls, selection, reading, focus, and unsaved drafts; they do not rebuild navigation or submit processing settings. Preference polling respects dirty selections and discards stale responses.

## Panel organization and headless operation

`settings.mjs` owns processing forms; `preferences.mjs` owns independent language selection; `activity.mjs` presents operations; `inspector.mjs` reads allowed tables. `navigation.mjs` owns destinations, categories, fragment history, compact menus, and dirty-draft confirmation. Chat content stays mounted across navigation. Runtime UI has no external dependencies or HTML interpretation of message data.

`run` remains headless without HTTP. `serve` is an owning in-process web alternative, not a remote client controlling another `run`. An external supervisor can run either as the sole owner. Backend transport/controller/HTTP tests cover cancellation and ownership; browser API interception does not replace those guarantees.

## Verification

Run `make check` before delivery. `make check-go` includes vet/race tests; `make check-ui` uses Node.js 24+ and native `node:test`. `make build` needs Go/C only; Node.js is neither a distribution nor runtime requirement.

Tests use temporary databases and fake transports/engines. They cover reservation/progress write failures, cancellation/priority, joined shutdown errors, audio cleanup, cancellation-ignoring engines, stale HTTP reads, rapid tab changes, language persistence/switching, revisions, aliases, batches, migrations, and resume.

For manual checks, use a separate synthetic archive with capture paused and no real session. Check tabs, search, quotes, audio, people, language, and settings on desktop and narrow screens. Real pairing/history still requires the holder's operational validation described in [Operation](operation.md).
