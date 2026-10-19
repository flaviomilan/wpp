# Verificações de qualidade

## Preparação

O desenvolvimento usa Go 1.26.9, compilador C para SQLite, Node.js 24 ou posterior e npm. O executável e o painel continuam funcionando apenas com Go e os programas locais de transcrição já configurados.

```bash
make tools
make check
make build
```

`make tools` instala em `.tools/bin` as versões fixadas de golangci-lint 2.11.4, gopls 0.22.0, deadcode de x/tools 0.43.0 e Gitleaks 8.30.1, além dos hooks locais de publicação. O download do scanner é conferido contra checksums fixados. `npm ci --ignore-scripts` instala ESLint 10.12.0 e c8 11.0.0 a partir do lockfile. A preparação requer rede; as verificações e a suíte padrão não usam serviços externos. Ferramentas não entram no módulo Go da aplicação. Diretórios de ferramentas, dependências e relatórios ficam fora do Git.

É possível selecionar executáveis com `GO`, `NODE`, `NPM`, `GOFMT`, `GOPLS`, `GOLANGCI_LINT` e `DEADCODE`. `GO` pode ser um caminho absoluto quando o compilador não estiver no PATH. Nenhuma verificação conecta a conta real ou modifica `data/` e `exports/`.

## Comandos

| Comando | Verificação |
|---|---|
| `make check` | Todas as verificações abaixo, incluindo cobertura |
| `make check-security` | Índice e histórico Git; em pacote sem Git, somente fontes do manifesto |
| `make test-security` | Testes Python de publicação e empacotamento |
| `make check-format` | gofmt sem modificar arquivos |
| `make check-lint` | Linters Go, ESLint e uso de módulos/exports do painel |
| `make check-diagnostics` | gopls e deadcode, rejeitando também diagnósticos com saída zero |
| `make check-go` | Suíte Go com detector de corrida, CLI instrumentada e cobertura consolidada |
| `make check-ui` | node:test, inventário completo e cobertura JavaScript |
| `make test` | Suíte Go com detector de corrida, sem verificar limites |
| `make build` | Compilação do executável, sem Node.js/npm |

## Critérios

Não pode haver função declarada própria com cobertura zero no relatório Go, nem função JavaScript não exercitada. O mínimo global é **90% dos statements Go** e **90% das linhas JavaScript**. Os limites usam contagens reais, sem arredondar um resultado inferior para aprovação. Coverage não substitui assertions: cada cenário deve verificar resultados observáveis, estado persistido ou comportamento de erro.

A medição Go descobre todos os pacotes próprios com `go list ./...` e usa essa lista em `-coverpkg`, incluindo chamadas realizadas pelos testes de outros pacotes. Apenas `node_modules/` é removido dessa lista: dependências npm podem distribuir código Go de terceiros, como flatted, que não pertence ao produto. Não há exclusão de pacote próprio. Também compila uma CLI instrumentada, executa um comando offline e um comando inválido em subprocessos e agrega os contadores por bloco. Blocos repetidos não duplicam o denominador. Os testes do entrypoint verificam saída e código de encerramento.

O JavaScript usa c8/V8 com `--all`: módulos nunca carregados não desaparecem do relatório. Um inventário independente rejeita arquivos ausentes. Configuração e fixtures de teste não são código do produto; os scripts executáveis de verificação também são medidos. A cobertura de linhas pode atingir 100% mesmo com alternativas de uma expressão ainda não exercitadas; os relatórios apresentam também branches.

`deadcode` parte do executável de produção, sem usar testes como raízes. ESLint identifica variáveis, imports e caminhos locais sem uso. A análise AST do painel verifica módulos e exports a partir de `app.js`; imports dos testes não justificam manter uma API. Análise estática pode conservar métodos por aproximação e imports de namespace; referências e revisão complementam essas ferramentas. Não há afirmação de prova matemática de ausência de código morto.

Medição da entrega em 14/10/2026: Go consolidado **91,41%**, sem funções declaradas com cobertura zero. Por pacote: CLI **91,5%**, arquivo **90,8%**, serviço **90,4%**, transcrição **95,0%**, HTTP **91,2%** e WhatsApp **95,6%**. JavaScript apresentou **100% de linhas e funções**, com **83,26% de branches do painel**; essa diferença mantém visível que nem todas as alternativas estão cobertas. Lint, gopls, análise de uso e detector de corrida passaram.

Os linters habilitados estão em `.golangci.yml`. Não há exclusões de pacotes ou presets que ocultem erros. Supressões pontuais exigem regra específica e explicação. Erros de preparação/limpeza dos testes também precisam ser verificados. Escritas HTTP após perda do cliente não podem gerar outra resposta nem registrar conteúdo privado; os descartes correspondentes são documentados no ponto de escrita.

## Relatórios e testes

`coverage/go.out`, `coverage/go-functions.txt` e `coverage/go-summary.json` apresentam os blocos, as funções e os resultados por pacote. `coverage/coverage-summary.json`, `coverage/coverage-final.json` e `coverage/lcov.info` apresentam JavaScript por módulo. São arquivos gerados; cada execução reconstrói os relatórios.

As suítes usam bancos temporários, protocolo simulado, transporte HTTP em memória e executáveis auxiliares de FFmpeg/Whisper. Cobrem QR, conexão/revogação, correlação de histórico, download, cancelamento, limites, recuperação, rollback SQLite, publicação e interfaces. As exportações JSONL são verificadas contra o JSON Schema e conteúdos esperados; testes de streaming garantem captura concorrente, publicação atômica e preservação do cursor incremental. Casos exclusivos do gerador Markdown foram retirados com o código correspondente.

Os próprios verificadores têm testes para função descoberta, arquivo omitido, limite inferior, blocos duplicados, export sem uso e diagnósticos de ferramentas que terminam com código zero. Esperas de concorrência usam sinais e `testing/synctest` onde possível; os testes de encerramento aguardam as goroutines que iniciam.

O teste `TestRealWhisperPipeline` continua opcional, configurado por `WPP_TEST_MODEL`, `WPP_TEST_WHISPER` e `WPP_TEST_AUDIO`. Ele não é necessário para passar o limite: o fluxo próprio de execução é coberto na suíte padrão. Nenhum teste automatizado comprova a disponibilidade do histórico de uma conta real ou a precisão acústica de um modelo real.

Validação da exportação exclusiva em JSONL (15/10/2026): `make check` passou com **90,92%** de statements Go consolidados, nenhuma função própria sem teste e **100%** de linhas/funções JavaScript.

## Navegador real com dados fictícios

`npm run test:browser` usa Playwright/Chromium e os assets reais do painel. Prepare o navegador com `npx playwright install chromium` (em Linux CI, `npx playwright install --with-deps chromium`). O servidor de teste escuta somente em `127.0.0.1:4199`, serve apenas assets públicos e todas as APIs são interceptadas com dados fictícios; não abre SQLite, sessão nem conexão WhatsApp. Não execute um servidor real nessa porta. As capturas em `docs/images/` são demonstrações, sem QR utilizável.

## Validação do redesign em 18/10/2026

- `make check build`: aprovado no Linux, com Go 1.26.9, CGO e ferramentas fixadas. Cobertura Go consolidada: **90,92%**, sem funções próprias com cobertura zero. JavaScript: **100% de linhas e funções**, 25 testes de unidade/ferramentas.
- `npm run test:browser`: **11 jornadas aprovadas**, com APIs fictícias, temas claro/escuro, larguras 360/768/1440, viewport equivalente à área útil de zoom 200%, movimento reduzido, teclado/Escape, retorno de foco, rascunho e erro de validação, histórico, leitura durante polling, ações contextuais e consulta com proprietário externo. Os tokens de texto/ações e controles têm verificações de contraste. A emulação de viewport não comprova todos os comportamentos de zoom/tecnologias assistivas de cada navegador; a revisão manual de acessibilidade continua recomendada antes de release.
- A fixture de retry foi estabilizada aguardando a recuperação inicial do workspace antes de criar duas fontes. A regressão de IDs iguais foi reproduzida antes da correção; depois, 20 execuções com `-race` passaram. Nenhum comportamento do backend foi alterado.
- Empacotamento: três testes Python verificam conteúdo por manifesto, reprodução byte a byte, rejeição de dados/symlinks/traversal e exigência de licença fora de modo draft.
- Unidade systemd: verificada em cópia temporária com executável fictício existente; não foi instalada nem iniciada. O caminho de instalação real depende de seguir o guia.

O ambiente local não executa macOS. O job correspondente foi preparado, mas precisa rodar no GitHub antes da publicação. Nenhuma conta real foi conectada e nenhuma disponibilidade de histórico foi certificada.

O pacote draft foi extraído em `/tmp/wpp-source-verification/wpp`; `make tools`, `make check build`, os 11 testes de navegador e os três testes de empacotamento passaram ali, sem reutilizar `.tools` ou `node_modules` do projeto original. O cache externo de downloads/compilação Go foi compartilhado; isso não é uma validação offline. Após o último ajuste de acessibilidade, `make check-ui build` foi repetido nas duas árvores. Os binários compilados com `-trimpath` tiveram SHA-256 idêntico. Um smoke test do executável real em diretório temporário verificou HTTP 200 dos assets/módulos novos, cookie local, captura inicial pausada e saída zero por SIGTERM.

A licença do código próprio foi definida como MIT, copyright Flávio Milan (flaviomilan), e o destino é `flaviomilan/wpp`. As políticas usam canais do GitHub sem exigir contato pessoal. A ativação do relato privado de vulnerabilidades e as verificações do repositório ficam no checklist de publicação: a conexão consultada retornou 404, sem alteração remota. Não há resultado de execução do GitHub Actions ou macOS local anunciado.

A revisão final de licença e identidade gerou `/tmp/wpp-source.tar.gz` sem `--draft`, com 177 arquivos incluindo `LICENSE`. O texto MIT foi comparado ao template padrão, a autoria e os links locais foram conferidos e os três testes Python passaram. O código, scripts, templates e configuração desse pacote foram comparados byte a byte com a árvore extraída anteriormente: são os mesmos que passaram nos checks completos e no navegador. Por se tratar apenas de licença/documentação nesta revisão, os checks Go/JavaScript não foram repetidos sem necessidade.

## Proteção da publicação em 18/10/2026

- A auditoria dos textos do commit original, incluindo os documentos de planejamento removidos e os metadados Git, não encontrou segredos reconhecidos pelo Gitleaks. Isso não comprova ausência de qualquer dado pessoal. O e-mail pessoal dos metadados foi substituído por GitHub `noreply` na nova raiz, preservando nome e copyright.
- `make check build` passou novamente no Linux: Go consolidado **90,92%**, JavaScript com **100% de linhas/funções** e **25 testes**. As **11 jornadas de navegador** também passaram, usando somente dados fictícios.
- Os **24 testes Python** passaram: índice diferente da cópia de trabalho, arquivo privado adicionado com força, segredo apagado em commit posterior, metadados e tags, banco renomeado, binário e tamanho, imagens e fixture por hash, indisponibilidade do scanner, supressão por comentário/ambiente, conteúdo Base64, histórico raso e substituições de objetos/commits por `git replace` e metadados Git acima do limite de texto. Os testes de empacotamento verificam reprodução e exclusão de arquivos fora do manifesto.
- O pacote com **184 arquivos** foi extraído em diretório novo. Os checks de publicação e os 24 testes Python passaram nessa cópia, sem metadados Git. O mesmo scanner previamente verificado foi copiado para a árvore extraída; esse ensaio não valida um novo download.

Os hooks, a CI e o empacotador compartilham o validador. Consulte [Segurança](../SECURITY.md) para limites, resposta a incidentes e revisão humana obrigatória. Os resultados acima são locais; não afirmam execução de macOS ou sucesso de um workflow remoto.

A primeira CI desta proteção identificou uma corrida no teste de foco: a restauração inicial da rota ainda podia focar o título após a ativação do link de salto. O teste agora aguarda a seção inicial estar visível antes de agir. As capturas do navegador passaram a ser geradas em `test-results/`, sem regravar imagens aprovadas na documentação. Não houve alteração do runtime.

O trace de outra execução com um worker mostrou o teste pressionando Escape antes de a nova transcrição abrir: o texto da abertura anterior ainda satisfazia a asserção. A jornada agora aguarda o diálogo visível antes de ler e o fechamento após Escape antes de tentar novamente. Cinco repetições dessa jornada passaram com um worker, preservando as asserções de comportamento.

O limite de 1 MiB também é aplicado às mensagens e metadados Git antes de chamar o scanner. Um teste com mensagem acima do limite e token fictício reproduziu o descarte silencioso por tamanho do scanner; a validação agora bloqueia esse candidato antecipadamente.
