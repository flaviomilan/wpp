# Contributing to wpp

Help make the local archive useful to more people. Software presentation supports English and Brazilian Portuguese, with English as the default; documentation is maintained in English. Before proposing behavior changes, explain the problem and expected outcome in an issue. Small changes can go directly to a pull request.

## Prepare the environment

Use Go 1.26 (toolchain 1.26.9), C/CGO, Node.js 24+, and npm. Linux/macOS are the operational targets; the systemd example is Linux-only. Never develop against a personal session or database.

```bash
make tools
make check
make build
npx playwright install chromium
npm run test:browser
python3 -m unittest discover -s scripts -p 'test_*.py'
```

`make tools` installs pinned tools in `.tools/bin`, npm dependencies from the lockfile, and this clone's publication hooks. If another `core.hooksPath` is set, installation stops so existing hooks can be integrated. `make check` covers publication, formatting, lint, diagnostics, dead code, races, and coverage. At least 90% Go statements and JavaScript lines are required, with no untested first-party functions; tests must establish behavior. Node/npm are not binary runtime dependencies.

To install only publication barriers, use `make tools-security hooks`. Set your GitHub `noreply` email with `git config --local user.email`, without changing global configuration. Run `make check-security test-security` after staging. Checks inspect staged blobs and all reachable commits; unstaged files do not replace index content. Shallow clones are rejected: run `git fetch --unshallow` before publication. `git replace` does not change audited objects. Without Git, an extracted package checks manifest-listed sources only and does not install hooks.

Standard tests use temporary databases and fake transports/engines. The browser harness serves assets and intercepts APIs; never open a real account to generate screenshots. See [Quality](docs/quality.md) and [Architecture](docs/architecture.md).

## Submit a change

- Keep changes focused, preserve CLI/HTTP contracts, and explain behavior changes.
- Include relevant tests and update guides when commands, screens, or guarantees change.
- Describe the problem, outcome, and actual validation commands in the PR template. Never claim unrun checks passed.
- Never attach real databases, sessions, QR codes, messages, media descriptors, tokens, or exports. Use fictitious IDs/content.
- Do not weaken quality gates to make checks pass. State limitations involving real accounts or another platform.

Every new tracked file must be reviewed and listed in `release-files.txt`. That positive list permits public source, not real data. The validator still blocks operational types/directories even if listed or force-added. Text has a 1 MiB limit. Binaries, symlinks, and submodules are rejected, except screenshots approved by path and SHA-256 in `publication-policy.json`.

Use only the synthetic browser harness for screenshots. Tests write `test-results/`, leaving approved `docs/images/` assets untouched. Inspect the complete image and metadata, copy only the reviewed result, then update its policy hash. The synthetic JSONL example also needs review and an exact hash, and its text still passes scanning. Never approve real-session imagery. Review manifests, policy, hooks, and workflows as carefully as security code. Never use directory exceptions, findings baselines, or suppression comments to release secrets.

Local hooks can be disabled and are not a Git server barrier. CI checks after upload. See [Security](SECURITY.md) for limits and incident handling.

## Translation catalogs

Use stable message identifiers in embedded English and Brazilian Portuguese catalogs. Keep named placeholders identical; tests verify keys, placeholders, count variants, and English fallback. Catalog messages use plain-text rendering. Never use source prose as an ID, translate stored messages/transcripts, or expose internal error causes.

Go catalogs live in `internal/localization`; browser catalogs are embedded in `internal/webui/assets/messages.mjs`. Update both for shared public message IDs and run parity tests. The `application_language` preference is independent of processing `language`, which selects audio recognition. Preference reads must not create a default row; writes must not overwrite capture/audio settings or require capture ownership. Language switches must preserve reading, focus, and drafts.

## License, authorship, and contact

The target repository is [flaviomilan/wpp](https://github.com/flaviomilan/wpp), maintained by [Flávio Milan (@flaviomilan)](https://github.com/flaviomilan). First-party contributions follow the [MIT license](LICENSE), preserving notices and author attribution. Do not add commercial-use restrictions or mandatory visual credits to MIT.

Dependencies are not relicensed; see [third-party notices](THIRD_PARTY_NOTICES.md), including GPL v3 requirements for a combined executable incorporating libsignal. Downloaded tools/models are excluded from the source package.

Use [Issues](https://github.com/flaviomilan/wpp/issues) for ordinary problems, [SECURITY.md](SECURITY.md) for vulnerabilities, and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for moderation. Channels use GitHub features without a mandatory public personal email. Remote activation/verification belongs to the [publication checklist](docs/release.md).
