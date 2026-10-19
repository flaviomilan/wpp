# Dependências e avisos de terceiros

Inventário da árvore fixada em `go.mod`/`go.sum`, preparado em 18/10/2026. Os textos anexados pertencem aos respectivos autores e não relicenciam o código próprio do wpp. O pacote de fontes não inclui cópias do código das dependências; Go as resolve pelas versões declaradas.

## Código próprio e distribuição do conjunto

O código próprio do wpp é MIT, copyright 2026 Flávio Milan (flaviomilan), conforme [LICENSE](LICENSE). Preserve esse aviso e os avisos de terceiros ao redistribuir. A MIT permite combinar esse código com componentes GPL-compatíveis; isso não remove as condições dos componentes ou do programa combinado.

`go.mau.fi/libsignal v0.2.2`, dependência transitiva de whatsmeow, contém GNU GPL versão 3 em seu arquivo LICENSE. whatsmeow e util contêm MPL 2.0. Para distribuir um executável que incorpora libsignal, cumpra a GPL v3 aplicável ao programa combinado, incluindo a disponibilização do código-fonte correspondente, e preserve os avisos e obrigações dos demais componentes. Não anuncie esse binário como exclusivamente MIT.

O pacote gerado por `scripts/package-source.py` contém fontes próprias e os avisos listados, não executáveis ou código baixado das dependências. Ele não constitui, sozinho, uma entrega de todo o código-fonte correspondente para um futuro binário GPL. Prepare essa entrega antes de publicar binários; ela não é produzida automaticamente nesta mudança.

Referências: [MIT na OSI](https://opensource.org/license/mit), [compatibilidade com GPL na FAQ da FSF](https://www.gnu.org/licenses/gpl-faq.en.html#WhatDoesCompatMean) e os textos originais abaixo. Não há substituição ou mudança de licença das dependências nesta entrega.

| Módulo | Versão | Textos disponíveis |
|---|---|---|
| `filippo.io/edwards25519` | `v1.2.0` | [LICENSE](docs/licenses/filippo.io_edwards25519-LICENSE) |
| `github.com/DATA-DOG/go-sqlmock` | `v1.5.2` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/agnivade/levenshtein` | `v1.2.1` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/andreyvit/diff` | `v0.0.0-20170406064948-c7f18ee00883` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/beeper/argo-go` | `v1.1.2` | [LICENSE](docs/licenses/github.com_beeper_argo-go-LICENSE) |
| `github.com/coder/websocket` | `v1.8.15` | [LICENSE.txt](docs/licenses/github.com_coder_websocket-LICENSE.txt) |
| `github.com/coreos/go-systemd/v22` | `v22.7.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/davecgh/go-spew` | `v1.1.1` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/dlclark/regexp2` | `v1.11.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/elliotchance/orderedmap/v3` | `v3.1.0` | [LICENSE](docs/licenses/github.com_elliotchance_orderedmap_v3-LICENSE) |
| `github.com/golang/protobuf` | `v1.5.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/google/go-cmp` | `v0.7.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/google/uuid` | `v1.6.0` | [LICENSE](docs/licenses/github.com_google_uuid-LICENSE) |
| `github.com/mattn/go-colorable` | `v0.1.14` | [LICENSE](docs/licenses/github.com_mattn_go-colorable-LICENSE) |
| `github.com/mattn/go-isatty` | `v0.0.20` | [LICENSE](docs/licenses/github.com_mattn_go-isatty-LICENSE) |
| `github.com/mattn/go-sqlite3` | `v1.14.52` | [LICENSE](docs/licenses/github.com_mattn_go-sqlite3-LICENSE) |
| `github.com/mdp/qrterminal/v3` | `v3.2.1` | [LICENSE](docs/licenses/github.com_mdp_qrterminal_v3-LICENSE) |
| `github.com/petermattis/goid` | `v0.0.0-20260820044319-269ab09b5261` | [LICENSE](docs/licenses/github.com_petermattis_goid-LICENSE) |
| `github.com/pkg/errors` | `v0.9.1` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/pmezard/go-difflib` | `v1.0.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/rs/xid` | `v1.6.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/rs/zerolog` | `v1.35.1` | [LICENSE](docs/licenses/github.com_rs_zerolog-LICENSE) |
| `github.com/santhosh-tekuri/jsonschema/v6` | `v6.0.2` | [LICENSE](docs/licenses/github.com_santhosh-tekuri_jsonschema_v6-LICENSE) |
| `github.com/sergi/go-diff` | `v1.3.1` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/stretchr/testify` | `v1.12.0` | Somente metadados; conferir se integrar uma distribuição final |
| `github.com/vektah/gqlparser/v2` | `v2.5.27` | [LICENSE](docs/licenses/github.com_vektah_gqlparser_v2-LICENSE) |
| `go.mau.fi/libsignal` | `v0.2.2` | [LICENSE](docs/licenses/go.mau.fi_libsignal-LICENSE) |
| `go.mau.fi/util` | `v0.10.1` | [LICENSE](docs/licenses/go.mau.fi_util-LICENSE) |
| `go.mau.fi/whatsmeow` | `v0.0.0-20261013142727-4cf8bc8e0aeb` | [LICENSE](docs/licenses/go.mau.fi_whatsmeow-LICENSE) |
| `golang.org/x/crypto` | `v0.57.0` | [LICENSE](docs/licenses/golang.org_x_crypto-LICENSE) |
| `golang.org/x/exp` | `v0.0.0-20260908205506-85c1c2202aba` | [LICENSE](docs/licenses/golang.org_x_exp-LICENSE) |
| `golang.org/x/mod` | `v0.41.0` | Somente metadados; conferir se integrar uma distribuição final |
| `golang.org/x/net` | `v0.59.0` | [LICENSE](docs/licenses/golang.org_x_net-LICENSE) |
| `golang.org/x/sync` | `v0.23.0` | [LICENSE](docs/licenses/golang.org_x_sync-LICENSE) |
| `golang.org/x/sys` | `v0.48.0` | [LICENSE](docs/licenses/golang.org_x_sys-LICENSE) |
| `golang.org/x/term` | `v0.46.0` | [LICENSE](docs/licenses/golang.org_x_term-LICENSE) |
| `golang.org/x/text` | `v0.42.0` | [LICENSE](docs/licenses/golang.org_x_text-LICENSE) |
| `golang.org/x/tools` | `v0.50.0` | Somente metadados; conferir se integrar uma distribuição final |
| `golang.org/x/tools/go/expect` | `v0.1.1-deprecated` | Somente metadados; conferir se integrar uma distribuição final |
| `golang.org/x/tools/go/packages/packagestest` | `v0.1.1-deprecated` | Somente metadados; conferir se integrar uma distribuição final |
| `google.golang.org/protobuf` | `v1.36.12` | [LICENSE](docs/licenses/google.golang.org_protobuf-LICENSE) |
| `gopkg.in/yaml.v3` | `v3.0.1` | Somente metadados; conferir se integrar uma distribuição final |
| `rsc.io/qr` | `v0.2.0` | [LICENSE](docs/licenses/rsc.io_qr-LICENSE) |

## Interface e desenvolvimento

Fontes são as do sistema operacional; não há fonte ou SF Symbols da Apple redistribuídos. Os screenshots são gerados com dados fictícios do próprio projeto e não contêm material de conversas reais. Não há SDK, logotipo ou imagem Apple no pacote.

Playwright (`@playwright/test` e `playwright-core`) é Apache-2.0; ESLint é MIT; c8 é ISC, conforme LICENSE dos pacotes fixados no lockfile. São ferramentas de desenvolvimento, instaladas separadamente e excluídas do pacote de fontes. Chromium, Whisper, FFmpeg e modelos não são incluídos: cada instalação deve observar as licenças da versão/modelo obtido. Não redistribuir o diretório `.tools` junto ao projeto.

Antes de oferecer binários, conferir os textos de todas as dependências efetivamente ligadas, preservar os avisos exigidos e disponibilizar fontes conforme as licenças aplicáveis. O inventário não substitui essa verificação da distribuição final.
