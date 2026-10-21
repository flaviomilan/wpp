# Dependencies and third-party notices

Inventory of the tree pinned in `go.mod`/`go.sum`, prepared on 18 October 2026. Attached texts belong to their authors and do not relicense wpp's first-party code. The source package excludes dependency source copies; Go resolves declared versions.

## First-party code and combined distribution

First-party wpp code is MIT, copyright 2026 Flávio Milan (flaviomilan), under [LICENSE](LICENSE). Preserve that notice and third-party notices when redistributing. MIT permits combination with GPL-compatible components without removing their conditions or those of the combined program.

`go.mau.fi/libsignal v0.2.2`, a transitive whatsmeow dependency, includes GNU GPL version 3 in its LICENSE. whatsmeow and util include MPL 2.0. Distribution of an executable incorporating libsignal must comply with GPL v3 for the combined program, including corresponding source availability, and retain other components' notices/obligations. Never describe that binary as MIT-only.

`scripts/package-source.py` packages first-party sources and listed notices, not executables or downloaded dependency code. By itself it is not all corresponding source for a future GPL binary. Prepare that source delivery before publishing binaries; it is not produced automatically by this change.

References: [MIT at OSI](https://opensource.org/license/mit), [FSF GPL compatibility FAQ](https://www.gnu.org/licenses/gpl-faq.en.html#WhatDoesCompatMean), and the original texts below. This distribution neither replaces nor changes dependency licenses.

| Module | Version | Available texts |
|---|---|---|
| `filippo.io/edwards25519` | `v1.2.0` | [LICENSE](docs/licenses/filippo.io_edwards25519-LICENSE) |
| `github.com/DATA-DOG/go-sqlmock` | `v1.5.2` | Metadata only; verify if included in a final distribution |
| `github.com/agnivade/levenshtein` | `v1.2.1` | Metadata only; verify if included in a final distribution |
| `github.com/andreyvit/diff` | `v0.0.0-20170406064948-c7f18ee00883` | Metadata only; verify if included in a final distribution |
| `github.com/beeper/argo-go` | `v1.1.2` | [LICENSE](docs/licenses/github.com_beeper_argo-go-LICENSE) |
| `github.com/coder/websocket` | `v1.8.15` | [LICENSE.txt](docs/licenses/github.com_coder_websocket-LICENSE.txt) |
| `github.com/coreos/go-systemd/v22` | `v22.7.0` | Metadata only; verify if included in a final distribution |
| `github.com/davecgh/go-spew` | `v1.1.1` | Metadata only; verify if included in a final distribution |
| `github.com/dlclark/regexp2` | `v1.11.0` | Metadata only; verify if included in a final distribution |
| `github.com/elliotchance/orderedmap/v3` | `v3.1.0` | [LICENSE](docs/licenses/github.com_elliotchance_orderedmap_v3-LICENSE) |
| `github.com/golang/protobuf` | `v1.5.0` | Metadata only; verify if included in a final distribution |
| `github.com/google/go-cmp` | `v0.7.0` | Metadata only; verify if included in a final distribution |
| `github.com/google/uuid` | `v1.6.0` | [LICENSE](docs/licenses/github.com_google_uuid-LICENSE) |
| `github.com/mattn/go-colorable` | `v0.1.14` | [LICENSE](docs/licenses/github.com_mattn_go-colorable-LICENSE) |
| `github.com/mattn/go-isatty` | `v0.0.20` | [LICENSE](docs/licenses/github.com_mattn_go-isatty-LICENSE) |
| `github.com/mattn/go-sqlite3` | `v1.14.52` | [LICENSE](docs/licenses/github.com_mattn_go-sqlite3-LICENSE) |
| `github.com/mdp/qrterminal/v3` | `v3.2.1` | [LICENSE](docs/licenses/github.com_mdp_qrterminal_v3-LICENSE) |
| `github.com/petermattis/goid` | `v0.0.0-20260820044319-269ab09b5261` | [LICENSE](docs/licenses/github.com_petermattis_goid-LICENSE) |
| `github.com/pkg/errors` | `v0.9.1` | Metadata only; verify if included in a final distribution |
| `github.com/pmezard/go-difflib` | `v1.0.0` | Metadata only; verify if included in a final distribution |
| `github.com/rs/xid` | `v1.6.0` | Metadata only; verify if included in a final distribution |
| `github.com/rs/zerolog` | `v1.35.1` | [LICENSE](docs/licenses/github.com_rs_zerolog-LICENSE) |
| `github.com/santhosh-tekuri/jsonschema/v6` | `v6.0.2` | [LICENSE](docs/licenses/github.com_santhosh-tekuri_jsonschema_v6-LICENSE) |
| `github.com/sergi/go-diff` | `v1.3.1` | Metadata only; verify if included in a final distribution |
| `github.com/stretchr/testify` | `v1.12.0` | Metadata only; verify if included in a final distribution |
| `github.com/vektah/gqlparser/v2` | `v2.5.27` | [LICENSE](docs/licenses/github.com_vektah_gqlparser_v2-LICENSE) |
| `go.mau.fi/libsignal` | `v0.2.2` | [LICENSE](docs/licenses/go.mau.fi_libsignal-LICENSE) |
| `go.mau.fi/util` | `v0.10.1` | [LICENSE](docs/licenses/go.mau.fi_util-LICENSE) |
| `go.mau.fi/whatsmeow` | `v0.0.0-20261013142727-4cf8bc8e0aeb` | [LICENSE](docs/licenses/go.mau.fi_whatsmeow-LICENSE) |
| `golang.org/x/crypto` | `v0.57.0` | [LICENSE](docs/licenses/golang.org_x_crypto-LICENSE) |
| `golang.org/x/exp` | `v0.0.0-20260908205506-85c1c2202aba` | [LICENSE](docs/licenses/golang.org_x_exp-LICENSE) |
| `golang.org/x/mod` | `v0.41.0` | Metadata only; verify if included in a final distribution |
| `golang.org/x/net` | `v0.59.0` | [LICENSE](docs/licenses/golang.org_x_net-LICENSE) |
| `golang.org/x/sync` | `v0.23.0` | [LICENSE](docs/licenses/golang.org_x_sync-LICENSE) |
| `golang.org/x/sys` | `v0.48.0` | [LICENSE](docs/licenses/golang.org_x_sys-LICENSE) |
| `golang.org/x/term` | `v0.46.0` | [LICENSE](docs/licenses/golang.org_x_term-LICENSE) |
| `golang.org/x/text` | `v0.42.0` | [LICENSE](docs/licenses/golang.org_x_text-LICENSE) |
| `golang.org/x/tools` | `v0.50.0` | Metadata only; verify if included in a final distribution |
| `golang.org/x/tools/go/expect` | `v0.1.1-deprecated` | Metadata only; verify if included in a final distribution |
| `golang.org/x/tools/go/packages/packagestest` | `v0.1.1-deprecated` | Metadata only; verify if included in a final distribution |
| `google.golang.org/protobuf` | `v1.36.12` | [LICENSE](docs/licenses/google.golang.org_protobuf-LICENSE) |
| `gopkg.in/yaml.v3` | `v3.0.1` | Metadata only; verify if included in a final distribution |
| `rsc.io/qr` | `v0.2.0` | [LICENSE](docs/licenses/rsc.io_qr-LICENSE) |

## Interface and development

Fonts come from the operating system; no Apple font or SF Symbols are redistributed. Screenshots use the project's own synthetic data and no real conversations. No Apple SDK, logo, or imagery is bundled.

Playwright (`@playwright/test` and `playwright-core`) is Apache-2.0, ESLint is MIT, and c8 is ISC according to their lockfile-pinned package licenses. They are separately installed development tools excluded from source packages. Chromium, Whisper, FFmpeg, and models are not bundled; installations must observe their actual version/model licenses. Never redistribute `.tools` with this project.

Before offering binaries, verify every actually linked dependency's license, retain required notices, and supply source under applicable terms. This inventory does not replace a final-distribution review.
