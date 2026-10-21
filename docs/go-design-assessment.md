# Go design assessment

> Historical assessment: Markdown references and benchmarks describe the assessed version. Current export is JSONL-only; see [JSONL events](jsonl-events.md).

Assessment of the 13–14 October 2026 code, guided by `golang-design-patterns`. Scope: constructors, state, concurrency, resources, cancellation, limits, data flow, and packages. The original findings are described below; fixes applied on 14 October are summarized first. Source locations are historical references, not current line numbers.

## Applied fixes

| Finding | Implementation and verification |
|---|---|
| Transitions after completion | Cancellation and phase persistence share one critical section. Progress for tasks removed from `active` is rejected. Tests cover late progress and four simultaneous cancellations. |
| HTTP shutdown | Admission stops before draining. The server awaits `Shutdown` and handler cleanup; after five seconds it closes remaining connections, waits for handlers, and reports timeout. Tests use HTTP over `net.Pipe`, without external network. |
| Model-validation cancellation | Hashing checks context before/after each 64 KiB read. Tests check interruption and identical digests. An already-blocked OS read still depends on returning. |
| Capacity | At most 24 distinct active operations and 32 waiters per queue. Duplicates return existing operations. Capacity rejection precedes persisted acceptance; tests check no orphan reservation and released capacity. |
| Export | A separate read-only SQLite connection uses a deferred WAL snapshot and streams messages/attempts into a temporary file. References read at most two message metadata records. Names, coverage, and content share the snapshot. Tests check identical historical Markdown, concurrent capture, pending generations, cancellation, and preserved prior files on failure. |

Export no longer holds all messages and complete Markdown in memory. Current message size and the name registry still affect memory. Export generation is marked after atomic publication, leaving later arrivals pending. Historical AI batches and schema 5 remained compatible at that time.

## Export measurements

Fixture: 2,000 fictitious messages containing Portuguese, emojis, and multiple lines. Six runs per version using Go 1.26.9, Linux amd64, and AMD Ryzen 7 5700G. Values are median Go heap bytes allocated per export (`B/op`), including file publication:

| Version | Allocated bytes per export |
|---|---:|
| Previous implementation | 169,662,384 |
| Initial streaming | 117,722,944 |
| Streaming with shared escaping rules | 44,511,064 |

Observed allocations fell approximately 74%. This is neither peak RAM nor memory allocated directly by SQLite in C, and does not establish statistically improved latency. Profiling found frequent `strings.Replacer` reconstruction; sharing immutable rules reduced allocations without changing escaping.

Historical benchmark invocation:

```bash
go test ./internal/archive -run '^$' -bench '^BenchmarkExportMarkdown$' -benchtime=1x -benchmem -count=6
```

That command names the historical Markdown benchmark, which is not part of current JSONL export. Build, vet, race tests, and six JavaScript tests passed after the fixes at that time.

## Original diagnosis

### 1. Cancellation can overwrite terminal state — P2

At historical `internal/service/operations.go:145`, `cancel` obtains/cancels a task under the mutex, releases it, then calls `update`. Between those steps the worker can persist `cancelled` and remove the task from `active`. `update` checks neither removal nor terminal state, permitting `running/cancelling` after completion and leaving the operation apparently active until resume repairs it.

An exploratory test with four concurrent cancellations reproduced this at iteration 17, using fake storage without WhatsApp or real databases; it was removed after assessment. This was a logical race: mutexes prevent concurrent memory access but not incorrect transition order.

Recommended correction: keep cancellation admission and transition persistence in the same critical section, or revalidate active membership before writing. Add terminal-state invariants and a deterministic cancellation/completion test.

### 2. HTTP shutdown does not await active requests — P2

At historical `internal/webui/server.go:73`, a goroutine calls `server.Shutdown` without awaiting/propagating its result. `server.Serve` can return when listeners close while handlers still run. `ServeControlled` returns to CLI, which can close controller/database during an accepted registration/control request.

Recommended correction: explicitly await shutdown before releasing dependencies; on timeout close remaining connections and define handler cleanup. Test a blocked request completing only after shutdown starts.

### 3. Model reads ignore cancellation — P2

At historical `internal/transcription/local.go:82`, `io.Copy` hashes the model before the version-query timeout and does not observe context. Cancelling settings validation or shutdown cannot interrupt hashing while `Controller.Close` waits. Delay depends on model size and storage speed.

Recommended correction: context checks before/between blocks, tested with a controlled reader. This improves cooperative cancellation without promising to interrupt an already-blocked OS call.

### 4. Operations and queues have unbounded admission — P2

Historical `internal/service/operations.go:47` starts one goroutine per distinct operation; `internal/service/queue.go:28` appends waiters without a limit. Deduplication covers identical requests rather than distinct sources/chats. One worker bounds concurrent execution but not waiting goroutines/requests.

Recommended correction: bound operations/waiters before persisted acceptance, return explicit queue-full feedback, and preserve deduplication/manual priority/cancellation. Test distinct sources and prove rejected requests leave no orphan operations.

### 5. Complete export memory grows with history — P3

Historical `internal/archive/store.go:616` loads all messages into a slice. `internal/archive/markdown.go:210` renders the entire snapshot in memory before atomic writing. Decoded messages and Markdown coexist while storage connection/transaction remain occupied. Cost grows despite separate AI batch limits.

Recommended correction: measure heap/duration for large groups, then stream a consistent snapshot to a temporary file while preserving order, atomicity, and generation marking. Pagination without a consistent view does not solve this correctly.

## Sound aspects and design recommendations

Initialization is explicit without opening connections/starting processing from first-party `init` functions. Small transport/engine interfaces support real/fake implementations. Shared Controller modules own state. The message model is transport-independent, assets are embedded, and the panel runs without Node.js. Transactions, stable sources, revisions, atomic files, file-based downloads, limits, timeouts, and temporary cleanup were appropriate. Queue cancellation/manual priority existed before the capacity/terminal-state fixes.

The module structure fits a local tool; prioritize lifecycle/limits rather than adding layers. `Options` provides explicit configuration and test injection. There was insufficient evidence that functional options would help; centralized constructor validation matters more. Named state/phase/kind types can reduce invalid combinations while retaining JSON/SQLite values, but types alone cannot prevent incorrect transitions. Compile-time assertions can document adapters. No DI container or generic abstraction was justified.

## Validation limits

Build, vet, race tests, and six JavaScript tests passed before assessment. The exploratory cancellation test found behavior absent from that suite. No large-group benchmark existed at diagnosis; finding 5 described implementation growth without invented volume/memory estimates.

Suggested order was operation transitions, HTTP shutdown, model cancellation, queue capacity, then export measurement/streaming. These historical results do not establish current test outcomes or real account coverage.
