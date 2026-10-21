# Local audio transcription

wpp keeps an audio metadata queue in SQLite. Audio is downloaded only while processing; the permanent result is text associated with the original message and stable evidence ID.

## Setup and execution

The portable installation lives in `.tools/audio` and does not change system directories. The script installs whisper.cpp 1.8.7 and downloads a multilingual model:

```bash
bash scripts/setup-audio.sh
```

The default is `large-v3-turbo`, approximately 1.51 GiB. Downloaded size and SHA-256 are verified against the model repository's published values. FFmpeg must be installed. Requirements include curl, tar, Python 3, make, and a C++ compiler. If CMake is missing on Linux x86_64/aarch64, the script installs a portable copy.

Build wpp, stop the previous capture with Ctrl+C, and start:

```bash
./wpp \
  --whisper-bin ./.tools/audio/whisper/build/bin/whisper-cli \
  --whisper-model ./.tools/audio/models/ggml-large-v3-turbo.bin \
  run
```

Global options precede `run`. The controller used by `serve`/`run` uses persisted settings or portable installation paths by default. `--whisper-model` with the other audio options overrides those settings for the run and enables transcription. Without a valid engine, the queue remains stored and text capture continues. Installation does not automatically connect the account.

```bash
./wpp audio status
./wpp audio retry SOURCE-ID
./wpp serve
```

Prefer `serve` without a parallel `run`. **Audio** offers filters, states, attempts, real processing phases, errors, full transcript details, and copy. **Settings > Transcription > Model and advanced options** validates the engine before saving; changes apply to subsequent audio. Pausing lets the current audio finish. A one-off sync processes a finite list for its chat even with continuous capture paused.

**Retry** is available after the first error and after permanent failure when metadata exists. In an owning panel it processes only the selected audio and respects the global transcription pause; CLI and consultation-mode panels only requeue. Explicit retries reset the attempt budget. Transcripts appear in **Chats**, text search, complete JSONL exports, and subsequent incremental event revisions.

## Quality and configuration

Recognition runs locally with whisper.cpp, a multilingual model, Portuguese audio by default, beam search size 5, and engine fallback enabled. Audio is converted to 16-bit mono PCM WAV at 16 kHz, without translation or noise-reduction filters. Application presentation defaults to English independently; choosing Portuguese or English in the panel does not change recognition.

To compare `large-v3` with the initial `large-v3-turbo` model, allow enough storage and processing time:

```bash
bash scripts/setup-audio.sh large-v3 .tools/audio-large
```

The script also accepts `medium`. `.en` models are inappropriate for Portuguese. Models do not guarantee fidelity for noise, names, or overlapping speech. Validate representative recordings before treating dates, values, or decisions as confirmed.

| Option | Default | Effect |
|---|---|---|
| `--whisper-model` | empty | Model path; enables processing |
| `--whisper-bin` | `whisper-cli` | Engine executable |
| `--ffmpeg-bin` | `ffmpeg` | Conversion executable |
| `--audio-language` | `pt` | Recognition language; `auto` detects |
| `--audio-threads` | `0` | Up to 8 CPUs automatically; explicit range 1–256 |
| `--audio-timeout` | `30m` | Maximum time per task |
| `--audio-vocabulary` | empty | Names and specific terms, up to 2048 bytes |

Example: `--audio-vocabulary "Aurora, Mariana, acceptance testing, DEM-023"`. Vocabulary guides recognition; it is not an instruction to summarize or invent decisions. Transcription follows the engine output without later rewriting by a language model.

Each result records text, segments with start/end milliseconds, detected language, model name and SHA-256, engine-reported version, and completion time in UTC. Message previews are bounded at 12,000 bytes; full text remains in storage and exports/evidence.

## Capture and queue states

Only actually archived messages from tracked chats enter the queue. Normalization extracts a minimal descriptor: available media path, decryption key, hashes, MIME type, size, and duration. It does not embed sound, waveform, or quoted message text.

Message and task are written in one transaction; replay does not duplicate tasks. Private metadata stays in `audio_jobs`, excluded from snapshots, batches, API responses, and the inspector. Protect the data directory and database as carefully as the session.

| State | Meaning |
|---|---|
| `waiting_metadata` | Known audio without download metadata |
| `pending` | Metadata available; waiting for processing |
| `processing` | Reserved by the worker |
| `done` | Text saved; private download metadata discarded |
| `failed` | Three actual attempts failed; retry available |
| `cancelled` | Message deleted or replaced before completion |

One worker per directory holds `audio-worker.lock`. It works serially outside capture transactions and checks for jobs every half second. Untracking a chat leaves its pending jobs waiting for tracking to resume.

Reservations have a token and deadline, so late completion cannot overwrite a new reservation. Failed attempts back off by two and four minutes. Disconnection and graceful shutdown do not consume the attempt budget. After acquiring the lock on restart, the worker clears temporary residue and immediately returns interrupted reservations to the queue; expiry also recovers abandoned reservations.

## Temporary audio

During a task, `data/tmp-audio/job-…/` contains the download, converted WAV, and output JSON. Directory permissions are 0700; the original file is 0600. The worker removes the directory after persistence, failure, or cancellation. Ctrl+C cancels subprocesses and removes temporary files. Forced termination, power loss, or filesystem failure can leave residue; the next worker run attempts cleanup. Normal deletion is not forensic erasure.

Limits are 64 MiB for downloads, 256 MiB for WAV, 16 MiB for engine JSON, and 4 MiB for transcript text. Downloads have a two-minute deadline; tasks use the configured timeout. Exceeding a limit fails the task instead of silently publishing a truncated transcript.

## Persistence, revisions, and AI consumption

Schema 7 migrations preserve queues, phases, settings, operations, IDs, and historical revisions. Exports use the JSONL event journal. Legacy audio without descriptors waits for metadata; receiving that audio again with a descriptor can make it ready. Expired or unavailable media is not guaranteed recoverable.

`CompleteAudio` records the transcript, an `update` change, export generation, and job completion in one transaction. It does not change the audio kind or overwrite `Message.Text`. If an earlier batch was acknowledged, the transcript arrives as a new revision in a subsequent batch. Pending batches retain their original bytes. Existing input limits still apply to long transcripts.

Consumers must recognize automatic transcription, retain uncertainty, and cite source ID/revision. wpp does not automatically turn speech into approval of a request. Deletions received during processing invalidate reservations so late recognition cannot resurrect a message. Previously published text remains in historical revisions under the evidence retention policy. Aliases preserve source association and reconcile duplicate jobs.

## Validation

`make check` covers transactional queues, replays, late metadata, migrations, cancellation, reservation expiry, retries, deletion, aliases, transcript preservation on identity merge, temporary cleanup, metadata privacy, search, and incremental events.

The optional real-engine test uses a sample recording without a WhatsApp account:

```bash
WPP_TEST_MODEL="$PWD/.tools/audio/models/ggml-large-v3-turbo.bin" \
WPP_TEST_WHISPER="$PWD/.tools/audio/whisper/build/bin/whisper-cli" \
WPP_TEST_AUDIO="$PWD/.tools/audio/whisper/samples/jfk.wav" \
go test -v ./internal/transcription -run TestRealWhisperPipeline -count=1
```

It confirms execution, persistence, model identity, and cleanup. Assess real Portuguese audio with representative chat recordings. References: [whisper.cpp](https://github.com/ggml-org/whisper.cpp), [models](https://huggingface.co/ggerganov/whisper.cpp), [whatsmeow media download](https://pkg.go.dev/go.mau.fi/whatsmeow#Client.DownloadToFile).

## Panel location

Open **Settings > Transcription**, or **Configure transcription** in Audio. Audio language and vocabulary are visible directly; model, executables, threads, and timeout are under **Model and advanced options**. Use **Save settings**. Errors retain the draft and previous persisted values; leaving with changes offers continuing or discarding. Automatic transcription is a separate control. **Settings > Advanced > Application language** changes only software presentation.
