# Contribuir com o wpp

Obrigado por ajudar a tornar o arquivo local mais útil. A interface e a documentação usam português brasileiro. Antes de propor mudanças de comportamento, descreva o problema e o resultado esperado em uma issue; mudanças pequenas podem seguir diretamente em pull request.

## Preparar o ambiente

Use Go 1.26 (toolchain 1.26.9), compilador C/CGO, Node.js 24+ e npm. Linux e macOS são os alvos; o exemplo systemd é somente Linux. Não use uma sessão ou banco pessoal para desenvolvimento.

```bash
make tools
make check
make build
npx playwright install chromium
npm run test:browser
python3 -m unittest discover -s scripts -p 'test_*.py'
```

`make tools` instala versões fixadas em `.tools/bin`, dependências npm a partir do lockfile e os hooks de publicação deste clone. Se já houver outro `core.hooksPath`, a instalação interrompe para que você integre os hooks existentes. `make check` verifica publicação, formato, lint, diagnósticos, código sem uso, corrida e cobertura. São exigidos ao menos 90% de statements Go e linhas JavaScript, sem funções próprias omitidas; os testes devem comprovar comportamento. Node/npm não são dependências de execução do binário.

Para instalar somente as barreiras de publicação, execute `make tools-security hooks`. Use seu endereço `noreply` do GitHub em `git config --local user.email`, sem alterar a configuração global. Execute `make check-security test-security` depois de preparar os arquivos para commit. O índice e todos os commits alcançáveis são examinados; arquivos ainda não preparados não substituem os blobs do índice na verificação. Clones rasos são recusados: execute `git fetch --unshallow` antes de publicar. Substituições de `git replace` não alteram os objetos auditados. Em um pacote extraído sem Git, o check examina somente as fontes do manifesto, e não instala hooks.

Os testes padrão usam bancos temporários e transporte/motor fictícios. O harness de navegador serve somente assets e intercepta todas as APIs; não abra a conta real para gerar screenshots. Confira [Qualidade](docs/qualidade.md) e [Arquitetura](docs/arquitetura.md).

## Enviar uma mudança

- Mantenha a alteração focada, preserve contratos CLI/HTTP e explique mudanças de comportamento.
- Inclua testes pertinentes e atualize os guias quando comandos, telas ou garantias mudarem.
- Informe problema, resultado e comandos de validação no modelo de PR. Não declare testes não executados como aprovados.
- Nunca anexe bancos, sessões, QR, mensagens, descritores de mídia, tokens ou exports reais. Use IDs e conteúdo fictícios.
- Não reduza gates de qualidade para fazer um check passar. Se depende de conta real ou de outra plataforma, indique a limitação.

Todo novo arquivo versionado deve ser revisado e incluído em `release-files.txt`. Essa lista permite fontes públicas; ela não autoriza dados reais. O validador continua bloqueando tipos e diretórios operacionais, mesmo que sejam incluídos no manifesto ou adicionados com `git add -f`. Textos têm limite de 1 MiB; binários, links e submódulos são recusados, exceto as capturas PNG aprovadas por caminho e SHA-256 em `publication-policy.json`.

Para alterar capturas, use exclusivamente o harness fictício de navegador. Os testes gravam imagens em `test-results/`, sem substituir as capturas aprovadas em `docs/images/`. Inspecione a imagem inteira e seus metadados, copie somente a captura revisada para a documentação e só então atualize seu hash na política. O exemplo JSONL sintético também exige revisão e hash exato; seus textos continuam passando pelo scanner. Nunca aprove uma imagem de uma sessão real. Revise mudanças no manifesto, política, hooks e workflow com o mesmo cuidado que código de segurança. Não use exceções por diretório, baselines de achados ou comentários de supressão para liberar segredos.

Hooks locais podem ser desativados; não representam uma barreira no servidor Git. A CI verifica depois do envio. Confira [Segurança](SECURITY.md) para os limites e o procedimento de incidente.

## Licença, autoria e contato

O repositório de destino é [flaviomilan/wpp](https://github.com/flaviomilan/wpp), mantido por [Flávio Milan (@flaviomilan)](https://github.com/flaviomilan). As contribuições ao código próprio seguem a [licença MIT](LICENSE), preservando avisos de copyright/licença e a atribuição dos autores. Não acrescente restrições de uso comercial nem exigência de crédito visual obrigatório à MIT.

Dependências não são relicenciadas: consulte [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md), inclusive as condições GPL v3 do executável combinado com libsignal. Ferramentas e modelos baixados não entram no pacote de fontes.

Use [Issues](https://github.com/flaviomilan/wpp/issues) para problemas comuns. Para vulnerabilidades, siga [SECURITY.md](SECURITY.md); para moderação, [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Os canais usam recursos do GitHub; nenhum e-mail pessoal é obrigatório. A ativação e a conferência dos recursos remotos fazem parte do [checklist de publicação](docs/lancamento.md).
