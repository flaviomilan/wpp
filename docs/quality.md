# Quality checks

## Setup

Development uses Go 1.26.9, a C compiler for SQLite, Node.js 24+, and npm. The executable/web panel run without Node.js; transcription uses separately configured local programs.

```bash
make tools
make check
make build
```

`make tools` installs pinned golangci-lint 2.11.4, gopls 0.22.0, deadcode from x/tools 0.43.0, Gitleaks 8.30.1, and publication hooks under `.tools/bin`. Scanner downloads are checksum-verified. `npm ci --ignore-scripts` installs ESLint 10.12.0 and c8 11.0.0 from the lockfile. Setup requires network; standard checks/tests use no external services. Tools are outside the application's Go module; tool/dependency/report directories are excluded from Git.

Executable overrides include `GO`, `NODE`, `NPM`, `GOFMT`, `GOPLS`, `GOLANGCI_LINT`, and `DEADCODE`. `GO` can be absolute when the compiler is absent from PATH. Checks never connect a real account or change operational `data/`/`exports/`.

## Commands

| Command | Check |
|---|---|
| `make check` | All checks below, including coverage |
| `make check-security` | Staged content and Git history; manifest-only sources without Git |
| `make test-security` | Python publication/package tests |
| `make check-format` | gofmt without editing files |
| `make check-lint` | Go linters, ESLint, panel module/export usage |
| `make check-diagnostics` | gopls/deadcode, including diagnostics despite exit status zero |
| `make check-go` | Race tests, instrumented CLI, consolidated Go coverage |
| `make check-ui` | node:test, complete inventory, JavaScript coverage |
| `make test` | Go race tests without coverage thresholds |
| `make build` | Executable build without Node/npm |

## Criteria

No first-party declared Go function can have zero coverage; every JavaScript function must be exercised. Global minimums are **90% Go statements** and **90% JavaScript lines**, using actual counts without rounding a below-threshold result upward. Assertions must check observable results, persisted state, or failures; coverage alone is insufficient.

Go inventory uses `go list ./...` and `-coverpkg` for every first-party package, including cross-package calls. Only `node_modules/` is filtered because npm packages such as flatted can contain third-party Go. The checker builds an instrumented CLI, runs offline/invalid commands as subprocesses, and merges block counters without double-counting denominators. Entry-point tests verify output and exit status.

JavaScript uses c8/V8 `--all`; unloaded modules remain visible. An independent inventory rejects omitted files. Fixtures/configuration are not product code, but executable checking scripts are measured. 100% lines can coexist with untested expression alternatives, so branch coverage is also reported.

`deadcode` starts from the production executable, not tests. ESLint checks unused variables/imports/local paths. Panel AST analysis starts at `app.js`, so test imports cannot justify unused APIs. Conservative analysis/namespace imports still need reference review; no mathematical proof of dead-code absence is claimed.

Linters are listed in `.golangci.yml` without first-party package/preset exclusions. Narrow suppressions require a named rule and explanation. Test setup/cleanup errors must also be checked. Failed HTTP writes cannot produce a second response or expose private data in logs; intentional discarded writes are documented in place.

## Reports and tests

Go reports are `coverage/go.out`, `coverage/go-functions.txt`, and `coverage/go-summary.json`; JavaScript reports include `coverage/coverage-summary.json`, `coverage/coverage-final.json`, and `coverage/lcov.info`. Reports are generated afresh each run.

Suites use temporary databases, fake protocol, in-memory HTTP transport, and helper FFmpeg/Whisper executables. They cover QR, connection/revocation, history correlation, downloads, cancellation, limits, recovery, SQLite rollback, publication, and interfaces. JSONL is checked against schema and expected content; streaming tests cover concurrent capture, atomic publication, and cursor preservation. Markdown-only tests were removed with their generator.

Checker tests cover discovered functions, missing files, thresholds, duplicate blocks, unused exports, and diagnostic text from zero-exit tools. Concurrency uses signals/`testing/synctest` where possible; shutdown tests wait for their goroutines. Optional `TestRealWhisperPipeline` uses `WPP_TEST_MODEL`, `WPP_TEST_WHISPER`, and `WPP_TEST_AUDIO`, but is not needed to meet standard coverage. No automated test proves real-account history availability or acoustic accuracy.

## Real browser with synthetic data

`npm run test:browser` runs actual panel assets in Playwright/Chromium. Install with `npx playwright install chromium` (Linux CI: `npx playwright install --with-deps chromium`). The harness listens only at `127.0.0.1:4199`, serves public assets, and intercepts every API with fictitious data. It opens no SQLite/session/WhatsApp connection. Do not run a real server on that port. Documentation images are synthetic demonstrations without usable QR codes; tests write candidate images to `test-results/` for review.

## Historical validation records

The results below describe their dated versions, not the current language change.

On 14 October 2026, consolidated Go coverage was **91.41%**, with no zero-covered declared functions: CLI **91.5%**, archive **90.8%**, service **90.4%**, transcription **95.0%**, HTTP **91.2%**, WhatsApp **95.6%**. JavaScript had **100% lines/functions**, with **83.26% panel branches**. Lint, gopls, usage analysis, and race detection passed. The JSONL-only export validation on 15 October passed `make check` with **90.92% Go statements**, no untested first-party functions, and **100% JavaScript lines/functions**.

### Redesign validation, 18 October 2026

- `make check build` passed on Linux with Go 1.26.9, CGO, and pinned tools: **90.92% Go**, no zero-covered functions, **100% JavaScript lines/functions**, and **25 unit/tool tests**.
- **11 browser journeys** passed using fake APIs, both themes, widths 360/768/1440, a viewport equivalent to the useful area at 200% zoom, reduced motion, keyboard/Escape, focus return, drafts/validation errors, history, reading during polling, contextual actions, and external-owner consultation. Contrast token checks covered text/actions/controls. Viewport emulation does not prove all browser zoom/assistive-technology behavior; manual accessibility review remained advisable before release.
- Retry fixture setup was stabilized by waiting for initial recovery before creating two sources. Duplicate IDs reproduced before correction; afterward, **20 race-detected repetitions** passed without backend behavior changes.
- Three Python package tests checked manifest content, byte reproducibility, data/symlink/traversal rejection, and required license outside draft mode.
- The systemd unit was checked in a temporary copy with an existing fake executable; no service was installed/started. Real installation depends on the guide.

macOS was not run locally; its GitHub job still needed execution. No real account was connected or history certified. The draft was extracted into `/tmp/wpp-source-verification/wpp`; fresh `make tools`, `make check build`, 11 browser tests, and three package tests passed without reusing the original `.tools`/`node_modules`. External Go download/build caches were shared, so this was not offline validation. After the final accessibility adjustment, `make check-ui build` was repeated in both trees; `-trimpath` binaries had identical SHA-256. A temporary-directory real-binary smoke test checked HTTP 200 for assets/modules, local cookie, initial paused capture, and zero-exit SIGTERM.

First-party licensing became MIT with Flávio Milan (flaviomilan) attribution and `flaviomilan/wpp` as destination. GitHub-based policies avoid mandatory personal contact details. A repository lookup returned 404; no remote setting changed, private reporting activation stayed pending, and no remote Actions/macOS success was claimed.

The final license/identity review generated a non-draft `/tmp/wpp-source.tar.gz` with **177 files**, including LICENSE. MIT was compared with its standard template, authorship/local links were checked, and three Python tests passed. Code/scripts/templates/configuration matched the previously validated extracted tree byte-for-byte. That documentation/license-only revision did not repeat unaffected Go/JavaScript checks.

### Publication protection, 18 October 2026

- Gitleaks recognized no secrets in the original commit's text, removed planning documents, or Git metadata. This did not prove absence of all personal information. Personal metadata email was replaced with GitHub `noreply` in the new root, preserving author/copyright.
- Linux `make check build` passed again: **90.92% Go**, **100% JavaScript lines/functions**, **25 tests**, plus **11 synthetic browser journeys**.
- **24 Python tests** passed for staged-versus-working content, force-added private files, secrets deleted in later commits, metadata/tags, renamed databases, binary/size limits, image/fixture hashes, unavailable scanner, comment/environment suppression, Base64 content, shallow history, `git replace` substitutions, and oversized Git metadata. Package tests checked reproducibility and exclusion of unlisted files.
- A **184-file** package was freshly extracted. Publication checks and 24 Python tests passed without Git metadata. The already-verified scanner was copied in, so that exercise did not validate a fresh scanner download.

Hooks/CI/packaging share the validator. See [Security](../SECURITY.md) for limits, incident response, and required human review. These results do not claim macOS or remote workflow execution.

The first CI run found a focus-test race: initial route restoration could still focus the title after the skip link activated. The test now waits for the initial section before interaction. Screenshot output moved to `test-results/` rather than rewriting approved documentation images; runtime did not change.

Another single-worker trace showed Escape pressed before the new transcript dialog opened, because text left from the prior opening satisfied an assertion. The journey now waits for visible/open and closed states explicitly. Five single-worker repetitions passed with behavior assertions retained.

The 1 MiB limit also applies to Git messages/metadata before scanning. A test using an oversized message and fictitious token reproduced silent scanner size-skipping; the validator now rejects that candidate first.

## Bilingual presentation validation, 21 October 2026

- Linux `make check build` passed with Go 1.26.9, CGO, Node.js 26.8.1, and the pinned checking tools. Consolidated Go statement coverage was **91.01%**, with no zero-covered first-party functions. JavaScript had **100% lines/functions** and **89.51% branches** overall; the unit/tool suite contained **33 tests**. Branch coverage remains distinct from line coverage.
- **19 Playwright/Chromium journeys** passed for English and Brazilian Portuguese, remembered selection, temporary override, consultation mode, both themes, widths 360/768/1440, keyboard/focus, and an actual CSS zoom of 200%. Language switching retains quotes, original source content, reading state, and unrelated drafts. Six synthetic English documentation images were visually reviewed; their PNG chunks contained only IHDR/IDAT/IEND, and reviewed hashes were updated without broadening publication exceptions.
- **24 Python publication/package tests** passed. The workspace scanner passed on current manifest-listed bytes, in addition to the normal index/history checks. The source package contains **203 reviewed files**, including the embedded catalogs and new tests.
- The package was extracted into a temporary tree without Git metadata. A fresh `npm ci --ignore-scripts` installed development dependencies there. `make check build test-security` and all **19 browser journeys** passed from the extracted sources. Go download/build caches were shared and previously verified checking binaries were copied into its excluded `.tools/bin`; this does not claim a fresh tool download or offline setup. Root/extracted `-trimpath` executables had identical SHA-256.
- A real-binary smoke test ran with an empty `PATH` and Portuguese `LANG`, proving that Node.js is unnecessary for execution. It checked the English default, persisted Portuguese shared with CLI, a non-persistent English override, language after process restart, embedded module delivery, unchanged display time zone, and graceful zero-exit SIGTERM. It used isolated temporary data without linking an account.
- Compatibility tests preserved archived text/transcripts, processing settings, capture ownership, published batch bytes, and consumer state across language changes. New recovery diagnostics use stable message identities; legacy diagnostic records remain unchanged. Public errors retain status classification and safe explanations, including unknown-message fallback without exposing parameters.

These are local Linux results. No real WhatsApp account was connected, no acoustic quality or account history coverage was certified, and macOS/GitHub Actions were not run in this environment. The final validation record changes documentation only; the packaged runtime/test sources are the ones verified above.
