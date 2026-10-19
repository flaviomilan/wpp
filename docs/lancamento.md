# Preparação do lançamento

O repositório de destino é `flaviomilan/wpp` e deve permanecer privado até concluir as verificações abaixo. A preparação de fontes não equivale a um lançamento público ou à instalação de serviços.

## Antes de tornar público

- [x] Código próprio sob MIT, copyright 2026 Flávio Milan (flaviomilan), com `LICENSE` no manifesto.
- [x] Inventariar avisos e distinguir MIT do código próprio de GPL v3/MPL das dependências em `THIRD_PARTY_NOTICES.md`.
- [ ] Antes de distribuir binários, preparar código-fonte correspondente e avisos do conjunto GPL v3; o pacote de fontes próprias não substitui essa entrega.
- [x] Definir destino `flaviomilan/wpp`, mantido por Flávio Milan, e preencher links em README e políticas. O módulo local Go continua `wpp`; não há caminho de instalação `go install github.com/...` anunciado.
- [ ] Conferir acesso, visibilidade e descrição de https://github.com/flaviomilan/wpp antes de tornar público.
- [ ] Ativar e testar Private vulnerability reporting para que **Security → Report a vulnerability** seja utilizável; atualizar SECURITY.md com o canal confirmado.
- [x] Identificar @flaviomilan como moderador e documentar Report content/Report abuse, incluindo o formulário privado existente do GitHub.
- [ ] Conferir a opção Report to repository admins no repositório público; o fallback GitHub Support já está documentado.
- [x] Retirar pendências de escolha de licença e identidade do README/CONTRIBUTING, mantendo explícitos os passos remotos de publicação.
- [ ] Inspecionar o manifesto e o pacote para dados privados, caminhos pessoais, sessões, exports, logs e ferramentas. Ignore não remove arquivos que já tenham sido rastreados.
- [ ] Conferir screenshot e exemplos somente fictícios, documentação e links relativos.
- [ ] Executar preparação e checks a partir das fontes limpas, com resultados registrados.
- [ ] Executar o workflow no GitHub e confirmar Linux e macOS; execução local Linux não comprova o job macOS.

## Pacote de fontes

O arquivo `release-files.txt` é um manifesto positivo de arquivos revisados. Arquivos fora dessa lista nunca entram no pacote. Não gere a lista varrendo o diretório de trabalho inteiro: bancos, `.aws`, ferramentas, configurações locais e dados reais devem permanecer locais.

```bash
make tools-security hooks
make check-security
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 scripts/package-source.py --draft --output /tmp/wpp-source-draft.tar.gz
```

`--draft` permite validação local de um snapshot ainda sem licença; a entrega atual já inclui `LICENSE` e pode ser empacotada sem essa opção. O pacote não é enviado automaticamente ao GitHub. Para preparar as fontes e inspecioná-las antes de resolver os passos remotos acima:

```bash
python3 scripts/package-source.py --output /tmp/wpp-source.tar.gz
tar -tzf /tmp/wpp-source.tar.gz
```

O empacotador aplica a política de publicação, inclusive scanner de segredos, limite de texto e rejeição de bancos renomeados, binários não aprovados, symlinks e caminhos privados. Capturas PNG e o JSONL sintético `docs/examples/events-1.0.jsonl` exigem caminho e hash aprovados. O arquivo é reproduzível para as mesmas fontes; não inclui dependências, executável, áudio ou modelos. Fontes limpas não devem conter sessões ou dados pessoais. Consulte [Segurança](../SECURITY.md) antes de alterar aprovações.

## Conferência em diretório limpo

Extraia o pacote em um diretório temporário novo, prepare Go/C, Node.js/npm e Python 3, então execute:

```bash
make tools
make check build
npx playwright install chromium
npm run test:browser
python3 -m unittest discover -s scripts -p 'test_*.py'
```

Os downloads de dependências precisam de rede. Os testes não dependem de conta WhatsApp ou bancos reais. A configuração do workflow em `.github/workflows/quality.yml` roda em pull requests sem secrets de mantenedor, com permissões de leitura. Não utiliza `pull_request_target` para executar código de forks. Consulte a configuração das actions [checkout](https://github.com/actions/checkout), [setup-go](https://github.com/actions/setup-go) e [setup-node](https://github.com/actions/setup-node) ao atualizar as versões.

## Evidências desta implementação

Os resultados e limitações da validação local são registrados em `docs/qualidade.md`. Checks simulados não comprovam pareamento ou disponibilidade de histórico de uma conta real. Um push para o repositório privado não representa um lançamento público.
