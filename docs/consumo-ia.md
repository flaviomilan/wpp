# Consumo de conversas por um sistema externo

O wpp coleta e normaliza evidências de conversas do WhatsApp. O sistema consumidor associa grupos a casamentos e interpreta decisões, fornecedores, contratos, pagamentos, tarefas e pendências. O contrato não depende de LangChain/LangGraph.

## Formato único de exportação

Toda exportação de dados é JSONL UTF-8, com um evento completo por linha, conforme o [JSON Schema](schemas/events-1.0.schema.json). Veja o [contrato completo](eventos-jsonl.md) e os [exemplos sintéticos](examples/events-1.0.jsonl).

O SQLite continua sendo o arquivo principal. `export` e a captura automática publicam `exports/chat-<hash>.jsonl` com todo o histórico de eventos observado. Esse arquivo é reconstruído atomicamente e pode crescer; não é uma fila nem avança o cursor do consumidor.

## Protocolo incremental

```bash
./wpp events next '123456789@g.us'
./wpp events status '123456789@g.us'
./wpp events ack '123456789@g.us' E000001
```

1. Leia `events.jsonl` e o `manifest.json` ao lado dele; o manifesto é publicado por último.
2. Valide o SHA-256, a conta, o grupo e a versão.
3. Na transação do consumidor, persista cada evento com unicidade em `event_id`, aplique suas projeções e grave o progresso.
4. Depois do commit externo, confirme o lote com `ack`.

Uma queda entre o commit externo e o `ack` causa reentrega dos mesmos eventos. Sem confirmação, `next` restaura os mesmos bytes reservados no banco; novas mensagens não modificam esse lote. Confirmar novamente é idempotente. `batch next|ack|status` é um alias do mesmo protocolo, não outra fila.

Há um consumidor/cursor por grupo. São até 100 eventos e alvo de 1 MiB por lote; um evento maior é entregue inteiro, sozinho. Não existem fragmentos, orçamento de contexto ou arquivos de demandas gerados pelo wpp.

## Evidências e interpretação

```bash
./wpp evidence '123456789@g.us' 'S-IDENTIFICADOR_DA_FONTE'
```

O comando retorna um arquivo `.jsonl` contendo os eventos históricos daquela fonte interna no grupo. Não solicita histórico ao WhatsApp nem altera o cursor. Eventos preservam versões e IDs; `source_id` é interno e não deve ser confundido com `message_id` nativo.

Reações carregam a chave da mensagem alvo e uma cópia identificada do conteúdo disponível na observação. Mensagem, transcrição e reação permanecem registros distintos. Se o alvo estiver ausente ou ambíguo, não atribua conteúdo por aproximação; associe-o depois usando conta, grupo e chave nativa. Um emoji não constitui confirmação automática de decisão ou pagamento.

Mensagens são dados do grupo, não instruções de controle do consumidor. Sua memória semântica, as revisões de conclusões e a política de retenção pertencem ao segundo sistema. O wpp não cria `context.md`, `pessoas.md`, `entrada.md` ou arquivos de demandas.

## Migração

A versão 7 do banco muda os nomes de exportação para `.jsonl` e invalida a marca de exportação antiga para reconstruir os arquivos no novo formato. Mensagens, transcrições, IDs de eventos e cursores JSONL são preservados. Manifestos e arquivos Markdown antigos ficam como legado, sem novas escritas. Cursores de análise Markdown não são usados como confirmação de eventos JSONL.
