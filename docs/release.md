# Launch preparation

The target repository is `flaviomilan/wpp`. Keep it private until the checks below are complete. Source preparation is neither a public launch nor service installation.

## Before making the repository public

- [x] First-party code is MIT, copyright 2026 Flávio Milan (flaviomilan), with `LICENSE` in the manifest.
- [x] Inventory notices and distinguish first-party MIT from dependency GPL v3/MPL in `THIRD_PARTY_NOTICES.md`.
- [ ] Before distributing binaries, prepare corresponding source and notices for the GPL v3 combination; the first-party source package is insufficient by itself.
- [x] Set `flaviomilan/wpp` and maintainer Flávio Milan in README/policies. The local Go module remains `wpp`; no `go install github.com/...` path is advertised.
- [ ] Verify repository access, visibility, and description before making it public.
- [ ] Enable and test Private vulnerability reporting so **Security > Report a vulnerability** is usable; update SECURITY.md with the confirmed channel.
- [x] Identify @flaviomilan as moderator and document Report content/Report abuse, including GitHub's existing private form.
- [ ] Verify Report to repository admins in the public repository; the GitHub Support fallback is documented.
- [x] Resolve license/identity placeholders in README/CONTRIBUTING while retaining explicit remote publication steps.
- [ ] Inspect the manifest/package for private data, personal paths, sessions, exports, logs, and tools. Ignore rules do not remove previously tracked content.
- [ ] Review synthetic-only images/examples, English documentation, language instructions, and relative links.
- [ ] Run setup/checks from clean sources and record actual results.
- [ ] Run GitHub workflows and confirm Linux/macOS; a local Linux run does not establish macOS success.

## Source package

`release-files.txt` is a positive manifest of reviewed files. Unlisted files never enter the package. Never generate it by indiscriminately scanning the working directory: databases, `.aws`, tools, local configuration, and real data remain private.

```bash
make tools-security hooks
make check-security
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 scripts/package-source.py --draft --output /tmp/wpp-source-draft.tar.gz
```

`--draft` permits local validation before a license exists. This tree includes `LICENSE` and can be packaged without that option. Packaging never uploads to GitHub:

```bash
python3 scripts/package-source.py --output /tmp/wpp-source.tar.gz
tar -tzf /tmp/wpp-source.tar.gz
```

Packaging applies publication policy, including secret scanning, text limits, and rejection of renamed databases, unapproved binaries, symlinks, and private paths. PNG screenshots and `docs/examples/events-1.0.jsonl` require approved paths/hashes. Archives are reproducible for identical source bytes and exclude dependencies, executables, audio, and models. Clean sources must contain no sessions/personal data. Review [Security](../SECURITY.md) before changing approvals.

## Clean-directory verification

Extract into a new temporary directory, prepare Go/C, Node/npm, and Python 3, then run:

```bash
make tools
make check build
npx playwright install chromium
npm run test:browser
python3 -m unittest discover -s scripts -p 'test_*.py'
```

Dependency downloads require network access. Tests need no real WhatsApp account/databases. `.github/workflows/quality.yml` runs on pull requests with read permissions and no maintainer secrets. It does not run fork code through `pull_request_target`. Review [checkout](https://github.com/actions/checkout), [setup-go](https://github.com/actions/setup-go), and [setup-node](https://github.com/actions/setup-node) configurations when changing versions.

## Validation evidence

Actual local results and limitations belong in `docs/quality.md`. Simulated checks do not establish real pairing or account history availability. Pushing to a private repository is not a public launch. None of these instructions automatically changes remote visibility or publishes a release.
