# Security

wpp stores private data from one WhatsApp account. HTTP is local (loopback), protected by Host, cookie, and origin checks; it does not provide authenticated remote access. Do not expose the panel through proxies/tunnels as if it had remote access control.

## Reporting vulnerabilities

The responsible maintainer is [Flávio Milan (@flaviomilan)](https://github.com/flaviomilan) of [flaviomilan/wpp](https://github.com/flaviomilan/wpp). The chosen channel is **GitHub private vulnerability reporting**, without requiring a public personal email address.

Use [Security > Report a vulnerability](https://github.com/flaviomilan/wpp/security/advisories/new) to report privately when enabled. Include version, system, a minimal reproduction with fictitious content, and observed impact. **Do not attach real databases, sessions, QR codes, tokens, messages, or exports. Do not open a public issue with exploitable details.**

If the form is unavailable, open only an [issue requesting activation of the private channel](https://github.com/flaviomilan/wpp/issues/new), without describing the vulnerability or including sensitive data. Wait for activation before sending details. This public activation request is not a private vulnerability report.

## Repository preparation

The repository remains private during launch preparation. Before announcing it publicly, the maintainer must enable **Private vulnerability reporting** under **Settings > Advanced Security**, when available, and test the form from another account. This document does not establish that reporting is enabled.

See GitHub's [private reporting instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately) and [repository configuration instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/configure-for-a-repository).

Only the current version is maintained during preparation. No response deadline or public release history is announced. Once public versions exist, this policy must identify supported versions.

## Data and dependencies

Keep databases, sessions, and exports outside version control, and back up with the process stopped. Logs and screenshots can also contain private data. Test in isolated fictitious directories. Check dependency notices/licenses before redistributing binaries, transcription engines, or models.

## Publication barriers

`release-files.txt`, hash approvals, and the publication validator are shared by hooks, CI, and packaging. `pre-commit` reads staged blobs; `pre-push` checks every commit reachable from pushed references, commit metadata, and annotated tag messages. Shallow clones are rejected and `git replace` cannot substitute inspected objects. Deleting a secret in the next commit is insufficient. Authors, committers, and taggers must use GitHub `noreply` addresses.

Gitleaks is pinned by version and checksums, retains its default rules, scans decoded content, and rejects suppression comments. An unavailable/incorrect scanner or execution failure blocks publication. Candidates are inspected in a restricted temporary directory; contents and findings are not printed or uploaded as CI artifacts. Source packages use the exact validated bytes without rereading files after the audit.

No scanner recognizes every name, phone number, message, or other personal information inserted into an allowed source file. Human review remains necessary. Hooks can be bypassed and CI runs after push, so these measures cannot guarantee prevention of every leak before GitHub receives it. Paid secret protection is not required. Publication protection does not replace a runtime application audit.

## If exposure occurs

1. Stop pushes and distribution. Do not copy the exposed value into issues, logs, comments, or public reports.
2. Revoke or rotate affected credentials before cleaning history. For personal information, assess access and the applicable incident procedure.
3. Remove material from all affected references and check a fresh clone. Rewriting changes hashes; contributors should reclone to avoid restoring old commits.
4. Local copies, forks, artifacts, pull-request references, and caches can survive rewriting. For published sensitive data, follow [GitHub's removal guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository), including private support contact when needed. Do not send data to support automatically.
