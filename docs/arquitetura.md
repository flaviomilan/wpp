# Arquitetura e manutenção

O executável mantém a interface pública em `cmd/wpp` e `internal/service.Controller`. A CLI e o HTTP compartilham essa coordenação, sem subprocessos. Esta reorganização mantém o esquema SQLite 7, os IDs de fontes e o conteúdo dos lotes publicados.

## Responsabilidades

| Módulo | Responsabilidade |
|---|---|
| `internal/archive` | Transações, mensagens, identidades, revisões, consultas, recuperação e arquivos gerados |
| `internal/whatsapp` | Adaptação do protocolo, eventos e sessão vinculada |
| `internal/transcription` | Processar um áudio reservado, executar o motor e limpar a área temporária |
| `internal/service` | Coordenar o ciclo de vida e as escolhas persistidas |
| `internal/webui` | Proteções HTTP, classificação de erros públicos e apresentação |

Dentro de `service`, módulos privados concentram o estado que administram: `operations.go` registra operações e cancelamento; `connection.go` controla conexão e seus usuários; `history.go` agenda recuperação; `audio.go` controla motor, configuração e worker; `exports.go` serializa exportações e conserva falhas visíveis. `queue.go` coordena admissão cancelável, com prioridade FIFO para solicitações manuais após o trabalho atual. O Controller expõe ações de uso, sem exigir que seus consumidores conheçam esses locks.

As interfaces de transporte e motor permitem implementações reais e simuladas. O armazenamento de operações possui uma interface privada para testar falhas de gravação. Outros acessos permanecem concretos: não há hierarquias genéricas para dependências que não variam.

## Persistência e falhas

Uma operação só é aceita depois de persistir sua reserva. Atualizações obrigatórias e conclusão verificam erros. Se o banco falhar, o estado em memória mantém a falha consultável pelo painel, interrompe novas reservas automáticas e cancela recuperação automática. Reiniciar não transforma falhas em sucesso: operações inacabadas são sinalizadas na retomada.

Erros combinados conservam suas causas; uma falha de persistência não desaparece porque a solicitação de histórico também falhou ou porque houve Ctrl+C. A CLI retorna falhas do encerramento, inclusive de exportação. Erros de download e motor ficam na tarefa de áudio; falhas de JSONL ficam no exportador e não desfazem mensagens já gravadas.

Cancelar uma solicitação na fila a remove sem esperar a tarefa atual. São aceitas até 24 operações distintas ativas; cada fila limita suas esperas a 32. Solicitações iguais continuam deduplicadas quando a capacidade está ocupada. O cancelamento é persistido na mesma região crítica da transição e nenhuma atualização de progresso pode reabrir uma tarefa concluída. Cancelamento não conclui uma transcrição mesmo se o motor retornar texto após ignorar seu contexto. A pausa global continua permitindo terminar o áudio já iniciado. O encerramento HTTP primeiro bloqueia a admissão e aguarda requisições já aceitas. Após cinco segundos de drain, fecha conexões restantes e aguarda a limpeza dos handlers antes de liberar suas dependências. O controlador cancela operações, aguarda workers, fecha a conexão, limpa temporários, registra interrupções, exporta pendências e libera o lock. A validação do modelo observa cancelamento entre blocos de leitura.

O HTTP centraliza a tradução dos erros do controlador: entrada inválida usa 400, conflito usa 409, registro inexistente usa 404 e falha interna usa 500 com explicação segura. Causas internas, conteúdo de mensagens e descritores privados não são incorporados a esses erros públicos.

## Consultas e interface

A exportação JSONL usa uma conexão SQLite separada, somente leitura, e um snapshot WAL com transação adiada. Percorre o diário de eventos, preservando uma linha completa por evento; libera o snapshot antes de marcar a geração exportada. Uma chegada concorrente, inclusive um snapshot de reações sem alteração do texto da mensagem, continua pendente. Arquivos completos não confirmam o cursor dos lotes.

A listagem de áudio lê itens, nomes e contagens em uma única transação SQLite. Não consulta nomes ou mensagens separadamente para cada cartão. Detalhes extensos continuam sendo carregados sob demanda. A elegibilidade para retry usa uma consulta de domínio independente da apresentação dos cartões.

O JavaScript é servido como módulos ES embutidos: `chats.mjs`, `audio.mjs`, `people.mjs`, `settings.mjs`, `activity.mjs`, `inspector.mjs` e `navigation.mjs`. `app.js` compõe os módulos; `api.mjs`, `dom.mjs` e `requests.mjs` concentram utilitários compartilhados. Não há bundler.

Cada fluxo de leitura recebe uma versão de solicitação. A navegação invalida leituras antigas; respostas fora de ordem não substituem seleção, filtros, mensagens ou detalhes recentes. Isso não cancela ações já aceitas pelo servidor. O estado de processamento pertence ao processo Go e continua existindo quando a aba fecha.

## Verificação

Execute `make check` antes de entregar mudanças. O comando reúne `make check-go` (vet e testes com detector de corrida) e `make check-ui` (Node.js 24 ou posterior, runner nativo `node:test`). `make build` compila somente Go; Node.js não é dependência de execução nem de distribuição.

Os testes usam bancos temporários, transporte e motores simulados. Incluem reservas e progresso com falhas, cancelamento e prioridade da fila, erros combinados no encerramento, limpeza de áudio, motor que ignora cancelamento, respostas HTTP fora de ordem e troca rápida de abas. Testes existentes cobrem revisões, aliases, lotes, migração e retomada.

Para validar o painel manualmente, use um diretório separado com mensagens fictícias, captura pausada e nenhuma sessão real. Confira abas, busca, citações, áudio, pessoas e ajustes em desktop e tela estreita. A disponibilidade de histórico e o pareamento de uma conta real continuam exigindo a validação operacional descrita em [Operação](operacao.md).

## Organização do painel

`settings.mjs` administra estado e formulário de ajustes; `activity.mjs` apresenta o snapshot de operações e `inspector.mjs` consulta as tabelas permitidas. `navigation.mjs` administra destinos, categorias, histórico de fragmentos, menu compacto e confirmação de saída com rascunho. O conteúdo das conversas permanece montado durante a navegação para conservar a leitura. O DOM recebe apenas texto; não há dependências de interface externas em runtime.

O modo `run` continua headless e não inicia HTTP. `serve` é a alternativa com controlador web no mesmo processo; não é um cliente de controle de outro `run`. Um supervisor externo pode executar qualquer um como único proprietário. Os testes de transporte/controlador e HTTP usam bancos temporários e cobrem cancelamento e conflito; os testes de navegador interceptam APIs e não substituem essas garantias de backend.
