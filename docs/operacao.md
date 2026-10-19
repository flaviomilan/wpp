# Operação e referência de comandos

O wpp usa SQLite como arquivo principal e JSONL como único formato de exportação de dados. Consulte [Funcionamento](funcionamento.md), [Interface](interface.md) e o [contrato JSONL](eventos-jsonl.md).

## Instalação e primeiro uso

Use Go 1.26, toolchain 1.26.9 e compilador C para SQLite. O alvo operacional usa `flock` em Linux/macOS. O build não depende de Node.js; as verificações de desenvolvimento usam Node.js 24+ e npm.

```bash
make build
./wpp serve
```

Em Ajustes, vincule a conta pelo QR, descubra conversas e acompanhe as desejadas. Escolha captura contínua ou sincronização pontual. Fechar a aba não encerra o processo; Ctrl+C encerra e conclui exportações pendentes. A primeira abertura começa pausada e as preferências ficam persistidas.

Alternativa pela CLI:

```bash
./wpp login
./wpp chats --groups
./wpp track '123456789@g.us'
./wpp run
```

Copie os IDs completos mostrados na listagem. Os IDs dos exemplos são fictícios. `run` trabalha em primeiro plano; um supervisor externo pode reiniciá-lo e encaminhar SIGTERM. Não execute dois controladores sobre o mesmo diretório.

## Referência de comandos

Sintaxe: `wpp [opções globais] comando [argumentos]`.

| Comando | Resultado |
|---|---|
| `login` | Vincula a conta pelo QR; reutiliza sessão existente |
| `chats [--groups]` | Descobre e lista conversas com seus IDs |
| `track <id>` / `untrack <id>` | Habilita/desabilita acompanhamento, preservando registros |
| `run` | Captura, recupera histórico, processa áudio e atualiza JSONL |
| `serve` | Painel e controlador compartilhado; consulta se o lock estiver ocupado |
| `status` | Cobertura observada e tentativas de recuperação |
| `export` | Reconstrói os arquivos completos `chat-<hash>.jsonl` |
| `events next <grupo>` | Publica ou restaura o lote JSONL pendente |
| `events ack <grupo> <lote>` | Confirma o lote após commit do consumidor |
| `events status <grupo>` | Cursor confirmado, lote pendente e eventos restantes |
| `batch next|ack|status ...` | Alias do mesmo protocolo de `events` |
| `evidence <grupo> <fonte>` | Publica o histórico dos eventos da fonte em JSONL |
| `audio status` | Resumo da fila de áudio |
| `audio retry <fonte>` | Reenfileira falha com descritor privado disponível |
| `audio correct <fonte> <arquivo.json>` | Registra uma correção versionada de transcrição |

`status`, acompanhamento, exportação, lotes, evidências e correções não conectam ao WhatsApp. Ainda abrem o SQLite e podem executar migrações. Lotes e evidências aceitam grupos `@g.us`; a exportação completa contempla as conversas arquivadas. A conta precisa estar vinculada para exportar mensagens existentes.

## Opções e diretórios

| Opção | Padrão / uso |
|---|---|
| `--data-dir` | `./data`: SQLite, sessão, locks e área temporária |
| `--export-dir` | `./exports`: arquivos completos, lotes e evidências JSONL |
| `--timezone` | `America/Sao_Paulo`: exibição no painel/status; eventos usam RFC 3339 UTC |
| `--listen` | `127.0.0.1:8080`: endereço do painel |
| `--whisper-model` | Caminho do modelo local; habilita transcrição em `run` |
| `--whisper-bin` / `--ffmpeg-bin` | Executáveis de reconhecimento e conversão |
| `--audio-language` | `pt`; use `auto` para detecção |
| `--audio-vocabulary` | Vocabulário de apoio, sem tradução |
| `--audio-threads` | `0`: seleção automática de até 8 CPUs |
| `--audio-timeout` | `30m`: limite por áudio |

```bash
./wpp --data-dir ./dados --export-dir ./exports run
./wpp --data-dir ./dados --export-dir ./exports export
```

Caminhos relativos partem do diretório de execução. Use os mesmos diretórios em captura e consumo. Os antigos parâmetros `--batch-max-bytes`, `--batch-max-messages`, `--context-max-bytes`, `--references-max-bytes` e `--evidence-revision` foram removidos com o exportador Markdown. Os lotes JSONL mantêm eventos inteiros, até 100 por lote e alvo de 1 MiB; um evento maior vai sozinho.

## Arquivos e responsabilidade

```text
data/
  archive.db             # mensagens, transcrições, eventos, reservas e cursores
  session.db             # sessão privada do WhatsApp
  tmp-audio/             # arquivos de trabalho temporários
exports/
  chat-<hash>.jsonl       # histórico completo, reconstruído automaticamente
  events/chat-<hash>/
    E000001/events.jsonl  # lote imutável
    E000001/manifest.json # versão, cursores, conta, grupo, quantidade e hash
    evidence/source-<hash>.jsonl
```

Os manifestos JSON são metadados de controle. Todos os registros exportados são JSONL no mesmo contrato versionado. O wpp não cria contexto, diretório de pessoas, entradas de lote ou demandas em Markdown.

O arquivo completo é atualizado após dois segundos sem mudanças. Se a conversa estiver continuamente ativa, pode aguardar uma pausa; o encerramento normal força a atualização. Ele contém histórico de eventos, inclusive anteriores a edições/exclusões, e não apenas mensagens no estado atual. Não modifique arquivos gerados: serão reconstruídos.

## Consumo e concorrência

```bash
./wpp events next '123456789@g.us'
# Verifique o manifesto; faça commit no consumidor com unicidade por event_id.
./wpp events ack '123456789@g.us' E000001
./wpp events status '123456789@g.us'
```

Há um cursor e um lote pendente por grupo. `events` e `batch` compartilham lock, cursor e arquivos; não são dois consumidores. Sem confirmação, `next` entrega os mesmos bytes. O manifesto é publicado por último e `ack` confere integridade. Arquivos completos e evidências não avançam esse cursor.

`run` e o controlador do painel usam lock exclusivo de captura. Lotes e evidências podem ser consultados enquanto a captura escreve: o SQLite mantém transações e o exportador completo usa leitura WAL separada. No painel, fechar a requisição não cancela operações já aceitas; o cancelamento é explícito.

## Atualização, backup e restauração

Pare o controlador antes de substituir o executável ou copiar os bancos. Faça backup de todo o diretório de dados e dos lotes publicados, preservando permissões. Copiar somente `archive.db` enquanto há escrita pode perder dados no WAL; use parada limpa ou backup consistente do SQLite.

A migração para o esquema 7 preserva mensagens, transcrições, fontes, IDs de eventos e cursores JSONL. Muda os nomes completos de `.md` para `.jsonl` e invalida a marca antiga para reconstruir a exportação. Binários anteriores rejeitam o banco atualizado; não há downgrade automático.

Arquivos Markdown e reservas antigas permanecem como legado, sem regeneração ou exclusão automática. Confirmações do antigo fluxo Markdown não viram confirmações JSONL. Reprocesse o fluxo JSONL usando `event_id` para deduplicar.

Para restaurar, use um backup consistente de dados e exports. Não misture bancos de contas diferentes; um diretório pertence a uma única conta. Preserve o banco para manter os IDs internos e as reservas do consumidor.

## Saída, falhas e limites

A CLI retorna código diferente de zero para falhas, inclusive falhas ao concluir exportações no encerramento. Uma falha de JSONL não desfaz mensagens já persistidas. Corrija o caminho/permissões e execute `export`; para lote incompleto, repita `events next` antes de confirmar.

`ack` declara consumo; não certifica a análise semântica nem a completude do WhatsApp. Reconexões, histórico indisponível, mensagens cifradas, edições e exclusões não observadas permanecem limitações. Não interpretar ausência de mensagens ou emojis como fatos de negócio.

Áudios são temporários; transcrições e sua proveniência ficam persistidas. Após sucesso, as chaves de download são descartadas. Veja [Transcrição](transcricao.md) para processamento e [Eventos JSONL](eventos-jsonl.md) para correções.

Sessão e descritores privados não são exportados. O histórico pode conter mensagens anteriores à exclusão: retenção e proteção das evidências devem considerar esse fato. Testes automatizados usam dados sintéticos e não comprovam cobertura de uma conta real.

## Validação com uma conta real

Use uma conversa de teste com consentimento dos participantes. Confira texto, resposta, menção, mídia, áudio, reação adicionada/alterada/removida e edição. Teste desconexão, reconexão e recuperação; compare os eventos com o celular sem presumir histórico completo. Valide os lotes e repita uma entrega para confirmar deduplicação no consumidor.

## Captura em background no Linux

O wpp trabalha em primeiro plano e não se daemoniza. Um supervisor externo pode executar `run` sem navegador. O exemplo [wpp-capture.service](../ops/wpp-capture.service) é uma unidade **de usuário** do systemd, não instalada automaticamente. Não requer root para instalar a unidade na conta atual.

Depois de compilar, prepare caminhos independentes do diretório do projeto:

```bash
install -Dm755 ./wpp "$HOME/.local/bin/wpp"
mkdir -p "$HOME/.local/share/wpp" "$HOME/.config/systemd/user"
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" login
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" chats
"$HOME/.local/bin/wpp" --data-dir "$HOME/.local/share/wpp/data" track 'ID_COMPLETO_EXIBIDO'
install -m644 ops/wpp-capture.service "$HOME/.config/systemd/user/wpp-capture.service"
systemctl --user daemon-reload
systemctl --user enable --now wpp-capture.service
```

Vincule a conta e acompanhe pelo menos uma conversa **antes** de iniciar. O serviço usa `%h/.local/share/wpp` como diretório de trabalho e caminhos explícitos para dados e exports. `%h` é expandido pelo systemd, não é uma variável de shell. Não copie os bancos de outra conta para esse diretório. Se já tem dados, ajuste os caminhos da unidade para o arquivo existente após backup e parada do proprietário.

```bash
systemctl --user status wpp-capture.service
journalctl --user -u wpp-capture.service -f
systemctl --user stop wpp-capture.service
systemctl --user restart wpp-capture.service
systemctl --user disable --now wpp-capture.service
```

Parar envia SIGTERM e aguarda até 60 segundos. A aplicação cancela operações, finaliza gravações aceitas e tenta concluir exports; depois do timeout, uma morte forçada pode deixar exports atrasados, reconstruídos do SQLite na próxima execução. `Restart=on-failure` reinicia falhas, não uma parada explícita. Logs permanecem no journal; compartilhe somente trechos sanitizados.

Para transcrição, instale Whisper/modelo/FFmpeg conforme [Transcrição](transcricao.md) e acrescente as opções globais com **caminhos absolutos** no `ExecStart`, antes de `run`. O exemplo funciona sem áudio; nenhum modelo da máquina do autor é obrigatório. Mudanças na unidade exigem `daemon-reload` e reinício.

O serviço de usuário segue a sessão do usuário. Para continuar após logout, o operador pode habilitar *linger* com `loginctl enable-linger "$USER"`, se permitido pela política da máquina; isso não é feito pelo projeto. Confira consumo de disco/CPU e política de backup antes de habilitar início automático.

### Usar o painel com um serviço

Com `run` ativo, `wpp --data-dir ... serve` abre em consulta: não passa a controlar esse processo. Para controle web, pare o serviço e execute `serve` com os mesmos caminhos. Você também pode supervisionar `serve` no lugar de `run` editando a unidade; mantenha o endereço em loopback. Nunca execute dois proprietários sobre o mesmo diretório ou apague o lock para forçar acesso.

Fechar a aba mantém o processo `serve` ativo; encerrar o processo interrompe processamento. `status`, cadastro de pessoas pelo painel e reenfileiramento permitido mantêm as capacidades descritas no [guia de interface](interface.md).

### macOS

A execução em primeiro plano de `run` e `serve` usa os mesmos comandos. O exemplo systemd é específico do Linux; para background no macOS, configure um agente `launchd` da sua conta com binário/diretórios absolutos, sinais e política de reinício equivalentes. O projeto não instala nem fornece uma unidade launchd nesta versão. Windows não é suportado.
