# Eventos JSONL para consumidores externos

O wpp entrega evidências, sem interpretar decisões, contratações, pagamentos ou o significado de emojis. Cada grupo permanece identificado pelo ID nativo; associá-lo a um casamento cabe ao consumidor. Não há dependência de LangChain/LangGraph.

O contrato público é [events-1.0.schema.json](schemas/events-1.0.schema.json), JSON Schema Draft 2020-12. Os [exemplos sintéticos](examples/events-1.0.jsonl) contêm um JSON completo por linha em UTF-8. Quebras de linha internas são escapadas; nenhum evento é fragmentado. Dados reais não são usados nos exemplos.

## Preparar, consumir e confirmar

```bash
./wpp events next '123456789@g.us'
# Retorna exports/events/chat-<hash>/E000001/events.jsonl
./wpp events status '123456789@g.us'
./wpp events ack '123456789@g.us' E000001
```

As opções globais `--data-dir` e `--export-dir` vêm antes de `events`. Os comandos são offline: usam apenas o arquivo SQLite, não solicitam histórico nem precisam conectar ao WhatsApp. A conta deve estar vinculada ao diretório. A captura continua usando `run`, o painel e o acompanhamento já configurado.

`./wpp export` e os botões do painel geram o histórico completo em `exports/chat-<hash>.jsonl`. A captura mantém esses arquivos automaticamente. `./wpp evidence <grupo> <fonte>` publica os eventos daquela fonte em um arquivo JSONL. Esses caminhos usam o mesmo contrato dos lotes, sem confirmar consumo. Não há gerador Markdown; arquivos antigos permanecem como legado. A mudança de nomes invalida a geração exportada antiga, preservando mensagens, transcrições e eventos.

1. Execute `next`; sem novidades ele informa que não há evento novo.
2. Leia `manifest.json` ao lado do JSONL. Ele é publicado por último. Verifique o SHA-256 dos bytes de `events.jsonl`, quantidade, conta, grupo e versão.
3. Na transação do consumidor, insira cada evento com unicidade em `event_id` e atualize suas projeções. Preserve o evento bruto para reprocessamento.
4. Confirme a transação do consumidor antes de executar `ack`.
5. Repita. Uma queda entre o commit externo e o `ack` resulta em reentrega; a unicidade impede duplicação.

Um lote pendente é uma reserva imutável no banco: novas mensagens não alteram seus bytes. Repetir `next` restaura JSONL e manifesto originais. `ack` rejeita arquivos diferentes da reserva e pode ser repetido sem avançar duas vezes. Lotes confirmados permanecem em disco para reprocessamento. Para reconstruir todo o consumidor, releia os lotes retidos e continue com `next`; não apague o banco de origem.

Há um cursor e um lote pendente por grupo. `batch` é um alias de `events`: compartilha os mesmos arquivos e confirmações. Exportações completas e consultas de evidências não avançam esse cursor. Não são gerados arquivos de contexto ou memória de demandas. Há até 100 eventos por lote e alvo de 1 MiB; um evento maior vai sozinho, completo. Isso é limite de tamanho do lote, não truncamento do conteúdo.

## Envelope, identidade e tempo

| Campo | Semântica |
|---|---|
| `schema_version` | String `1.0`; independente da versão dos antigos manifestos Markdown |
| `event_id` | Identificador interno estável da ocorrência; deduplicar por ele |
| `sequence` | Ordem local de persistência, com possíveis intervalos entre grupos |
| `event_type` | Tipo do acontecimento ou enriquecimento observado |
| `source` | Sempre `whatsapp` |
| `account_id` | JID da conta vinculada, sem componente de dispositivo |
| `group_id` | ID nativo do grupo |
| `message_id` | ID nativo da mensagem; em reações, o da mensagem alvo |
| `occurred_at` | Timestamp do acontecimento na origem, ou `null` |
| `observed_at` | Primeira observação persistida desta ocorrência, ou `null` em dados legados |
| `provenance` | Origem, indicação de derivação e limitações explícitas |
| `payload` | Dados específicos, definidos pelo schema |

Timestamps são RFC 3339 em UTC, com timezone `Z`. Data de exportação ou migração nunca substitui data da origem. O epoch usado por renderizadores legados para representar ausência é exportado como `null`.

`message_id` sozinho não é uma chave global. Use `(account_id, group_id, native_key.message_id, native_key.sender_id, native_key.from_me)`. Em reações, use a chave equivalente em `target_message`. O ator da reação **não** é o autor da mensagem alvo. `reaction_message_id` identifica a ocorrência nativa da reação.

`source_id` identifica a fonte interna arquivada; `content_version` identifica sua revisão. Esses valores não são IDs WhatsApp. IDs de eventos são persistidos e estáveis nas reexportações do mesmo arquivo. Preserve/restaure o SQLite junto dos backups: reconstruir um arquivo independente pode criar outros IDs internos.

`participant_id` é a identidade local canonicalizada somente com evidência de equivalência. `native_id` e `aliases` preservam os identificadores observados. Não unifique pessoas apenas por nome ou por semelhança dos IDs. Um alias comprovado posteriormente pode exigir reconciliar projeções pelo ID nativo; eventos antigos não são reescritos.

Nomes são os disponíveis na observação, não o cadastro atual consultado ao exportar. Um nome de grupo obtido posteriormente não é apresentado como conhecido em uma mensagem antiga. Nomes ou campos desconhecidos ficam nulos.

## Conteúdo e transcrição

Mensagens separam `text`, `caption`, `reply.quoted_text`, menções, encaminhamento e anexos. Texto é o conteúdo original, sem resumo. Tipos públicos: `text`, `audio`, `image`, `video`, `document`, `other`; `original_type` mantém o subtipo normalizado do coletor. Não há OCR nesta integração.

`attachments: null` significa metadados indisponíveis em registros antigos; `[]` significa nenhum anexo conhecido pelo normalizador para esse conteúdo. Os metadados permitidos incluem nome, MIME, tamanho e SHA-256 da origem. `file_reference` fica nulo se nenhum arquivo persistente estiver disponível. Não há download novo de imagens, vídeos ou documentos.

O áudio original e os arquivos de trabalho continuam temporários. Sua identidade está em `attachment_id` e, quando disponível, nos metadados e hash do anexo. Não existe arquivo persistente escondido atrás de `file_reference: null`; não é possível reabrir o áudio por esse campo.

Quando o Whisper conclui, `message.transcribed` entrega texto, idioma, modelo, hash do modelo, mecanismo/versão, `processed_at`, segmentos em milissegundos e `transcription_version`. O texto original da mensagem não é substituído. `occurred_at` é nulo porque a transcrição é um processamento local; datas de processamento e observação são separadas. Precisão acústica depende do áudio, modelo e vocabulário; o contrato não certifica que a fala foi reconhecida corretamente.

Para registrar uma correção fornecida pelo operador:

```bash
./wpp audio correct 'S-<fonte-do-audio>' transcricao.json
```

O arquivo contém um único objeto JSON, até 16 MiB:

```json
{
  "text": "A degustação será domingo às dez.",
  "language": "pt",
  "model": "ggml-large-v3-turbo.bin",
  "model_sha256": "hash-do-modelo-quando-conhecido",
  "engine": "correcao-manual",
  "processed_at": "2026-10-15T18:40:00Z",
  "segments": [{"start_ms": 0, "end_ms": 2800, "text": "A degustação será domingo às dez."}]
}
```

`text` deve ser não vazio e ter até 4 MiB; tempos de segmentos devem ser válidos. Metadados desconhecidos podem ser omitidos; não atribua um modelo a trabalho que ele não realizou. O áudio precisa ter transcrição existente e não estar excluído. A correção gera outra versão, mantém a anterior e identifica a proveniência como `correction`. Repetir o mesmo conteúdo e metadados não cria outra versão; alterar a data de processamento declara um processamento diferente. `audio retry` continua dedicado a falhas com descritor privado ainda disponível. Após sucesso, as chaves de download são apagadas: uma nova execução automática não é garantida sem acesso renovado ao original.

## Reações e vínculos tardios

`reaction.added`, `reaction.updated` e `reaction.removed` preservam ator, nome observado, Unicode integral do emoji, ocorrência nativa e chave alvo. Remoção explícita usa `emoji: ""`; emoji desconhecido usa `null`. Nunca interpretar a string de apresentação legada “Reação removida” como emoji.

Quando não há timestamp próprio da reação, mas o envelope da mensagem traz a data da ocorrência, `occurred_at` usa esse dado e registra `reaction_time_from_envelope`. Snapshots nunca usam a data da mensagem alvo como data da reação.

Uma primeira reação positiva é uma adição observada; `previous_state_known: false` deixa claro que não prova ausência de reação anterior. Uma alteração reconhecida pela comparação com uma ocorrência individual anterior leva `derived: true`. Essa comparação só usa estado anterior com timestamps ordenáveis; sem isso, o sistema não afirma conhecer a reação anterior. Ocorrências antigas não fazem o estado mais recente regredir.

`target_message` é uma **cópia congelada** do conteúdo local na observação: texto, legenda, transcrição e respectivas versões. Ela complementa o evento original e não o substitui. Uma edição posterior da mensagem ou correção posterior da transcrição não altera esse snapshot.

Se o alvo não foi recebido, o snapshot tem `content_status: "unavailable"`, conteúdo e versões nulos, mas mantém a chave disponível. Quando a mensagem chegar, associe-a usando conta, grupo e chave alvo. Se o autor estiver ausente, resolva somente quando houver correspondência única; `ambiguous` exige manter a incerteza. Para obter o estado atual de um áudio, consulte a transcrição mais recente vinculada à sua fonte, sem alterar o snapshot da reação.

Listas fornecidas pelo histórico geram `reaction.snapshot`: `reactions` inclui os participantes, emojis e tempos disponíveis, sem reconstruir ações anteriores. `completeness` pode ser `complete`, `partial` ou `unknown`; a integração atual normalmente só permite `unknown`. Nenhuma transição é derivada comparando listas.

`reactions: []` com disponibilidade conhecida representa lista vazia. `reactions: null` e `availability: "unavailable"` representa indisponibilidade. A ausência de elementos num campo protobuf repetido não comprova lista vazia: nesse caso a mensagem registra `reactions_availability: "unavailable"`, sem inventar um snapshot vazio. Reações cifradas ou legadas sem conteúdo disponível geram snapshot indisponível.

## Suporte efetivo, migração e limites

| Evento | Origem/limite |
|---|---|
| `message.created` | Primeira versão disponível localmente, inclusive importada |
| `message.edited` / `message.deleted` | Protocolos de edição/revogação observados; não garante observar todas as mudanças |
| `message.enriched` | Recuperação de conteúdo ou metadados sem edição comprovada |
| `message.transcribed` | Conclusão local ou correção explícita; versões anteriores preservadas |
| `reaction.added` / `updated` / `removed` | Mensagens individuais de reação disponíveis; alteração pode ser derivada e identificada |
| `reaction.snapshot` | Listas do histórico ou reação cujo conteúdo está indisponível |

A versão fixada de whatsmeow expõe `ReactionMessage` com chave, texto e timestamp opcional e `WebMessageInfo.reactions` no histórico. O wpp passa a consumir essas estruturas e tenta a decifragem local com `DecryptReaction` quando a sessão dispõe do segredo da mensagem. Falhas preservam a observação indisponível, sem expor o segredo. Ele não declara completude da lista, cobertura integral do histórico ou disponibilidade de reação cifrada. Os protocolos estão em [waE2E](https://github.com/tulir/whatsmeow/blob/4cf8bc8e0aeb/proto/waE2E/WAWebProtobufsE2E.proto) e [waWeb](https://github.com/tulir/whatsmeow/blob/4cf8bc8e0aeb/proto/waWeb/WAWebProtobufsWeb.proto).

A migração aditiva eleva o banco para a versão 7, impedindo que versões antigas do programa escrevam silenciosamente num arquivo atualizado. Faça backup do diretório de dados antes de atualizar; não há downgrade automático. Ela reutiliza as revisões de `changes` uma vez por fonte/revisão. Dados antigos sem observação registrada recebem `observed_at: null`. Não é possível recuperar retrospectivamente nomes históricos, menções descartadas, data exata de reações ou o conteúdo disponível no instante de uma reação antiga. Esse último snapshot permanece indisponível; não é preenchido com o texto atual fingindo ser histórico.

Um protocolo explícito de edição/exclusão recebido antes do original gera o evento correspondente com data original de envio nula; a chegada posterior do original enriquece o vínculo. Um registro importado já editado/excluído pode começar com `message.created` e seus indicadores, acompanhado de limitação; isso não afirma que a criação e a mutação foram observadas separadamente. `provenance.origin` distingue `live`, `history`, `recovered`, `migration`, `processing` e `correction`.

A ordem de entrega é a ordem local, inclusive após reconexão e recuperação. `ack` confirma consumo, não completude do WhatsApp. Eventos de exclusão não apagam revisões anteriores. O consumidor deve aplicar as versões posteriores para representar o estado atual e preservar as anteriores quando precisar de auditoria.

O JSONL usa lista explícita de campos públicos. Não contém sessão, chaves de mídia, descritores privados, bytes binários/base64 ou URLs temporárias de download. O consumidor continua responsável pela interpretação do negócio e pela política de retenção das evidências.
