# Avaliação de padrões Go

> Avaliação histórica: referências a Markdown e benchmarks antigos descrevem a versão avaliada. A exportação atual é exclusivamente JSONL; consulte [Eventos JSONL](eventos-jsonl.md).
Avaliação do código atual em 13–14/10/2026, orientada pela skill `golang-design-patterns`. Escopo: construtores, estado, concorrência, recursos, cancelamento, limites, fluxo de dados e organização dos pacotes. O diagnóstico original está preservado abaixo. As correções foram aplicadas em 14/10/2026 e estão resumidas nesta seção.

## Correções aplicadas

| Achado | Implementação e verificação |
|---|---|
| Transições após conclusão | Cancelamento e gravação da etapa ocorrem sob a mesma região crítica. Progresso de tarefas removidas de `active` é rejeitado. Testes cobrem progresso tardio e quatro cancelamentos simultâneos. |
| Encerramento HTTP | A admissão de novas requisições para antes do drain; o servidor aguarda `Shutdown` e a limpeza dos handlers. Após cinco segundos, fecha conexões restantes, aguarda os handlers e propaga o timeout. Testes usam HTTP sobre `net.Pipe`, sem rede externa. |
| Cancelamento da validação do modelo | O hash verifica o contexto antes e depois de cada leitura, usando blocos de 64 KiB. Testes verificam interrupção e digest idêntico. Uma leitura já bloqueada no sistema operacional continua dependendo de seu retorno. |
| Capacidade | Até 24 operações distintas ativas e 32 esperas por fila. Duplicatas continuam devolvendo a operação existente. Rejeição por capacidade ocorre antes da reserva persistida. Testes verificam ausência de operações órfãs e liberação de capacidade. |
| Exportação | Leitor SQLite separado, somente leitura, com transação adiada e snapshot WAL. Mensagens e tentativas são renderizadas incrementalmente no temporário; referências consultam no máximo dois metadados de mensagens. Nomes, cobertura e conteúdo pertencem ao mesmo snapshot. Testes verificam Markdown idêntico, captura durante a escrita, geração pendente, cancelamento e preservação do arquivo anterior em falhas. |

A exportação não mantém todas as mensagens nem o Markdown completo em memória. O tamanho da mensagem atual e o cadastro de nomes ainda influenciam o uso de memória. A marcação da geração exportada acontece depois da publicação atômica; chegadas posteriores continuam pendentes. Os lotes de IA e o esquema 5 permanecem compatíveis.

## Medição da exportação

Fixture: 2.000 mensagens fictícias, com português, emojis e múltiplas linhas. Seis execuções por versão, mesmo Go 1.26.9, Linux amd64 e AMD Ryzen 7 5700G. Valores abaixo são medianas de bytes alocados no heap Go por exportação (`B/op`), incluindo a publicação do arquivo:

| Versão | Bytes alocados por exportação |
|---|---:|
| Implementação anterior | 169.662.384 |
| Streaming inicial | 117.722.944 |
| Streaming e regras de escape compartilhadas | 44.511.064 |

A redução observada de alocações foi aproximadamente 74%. Isso não é uma medição do pico de RAM nem inclui memória alocada diretamente pelo SQLite em C. Não há afirmação de melhoria estatística de latência. O perfil inicial identificou reconstrução frequente de `strings.Replacer`; compartilhar as regras imutáveis reduziu essa alocação sem mudar o escaping.

Para medir a implementação atual:

```bash
go test ./internal/archive -run '^$' -bench '^BenchmarkExportMarkdown$' -benchtime=1x -benchmem -count=6
```

Compilação, `go vet`, suíte Go com detector de corrida e seis testes JavaScript passaram após as correções.

## Diagnóstico original

### 1. Estado terminal pode ser sobrescrito pelo cancelamento — P2

Em `internal/service/operations.go:145`, `cancel` obtém a tarefa e cancela seu contexto sob o mutex, solta o mutex e chama `update`. Nesse intervalo, a goroutine de execução pode persistir `cancelled` e remover a tarefa de `active`. `update` não verifica essa remoção nem protege estados terminais, e pode persistir `running/cancelling` depois da conclusão. A operação fica apresentada como ativa até uma retomada corrigir seu estado.

Um teste exploratório com quatro cancelamentos simultâneos reproduziu esse resultado na iteração 17. O teste usou armazenamento simulado, sem WhatsApp nem banco real, e foi removido depois da avaliação. Trata-se de uma corrida lógica; mutexes evitam acesso simultâneo à memória, mas não garantem a ordem das transições.

Correção indicada: manter admissão do cancelamento e persistência de sua transição na mesma região crítica, ou verificar novamente que a tarefa ainda está ativa antes de escrever. Acrescentar invariantes que impeçam alterações após estado terminal e um teste determinístico de cancelamento concorrente com conclusão.

### 2. Encerramento HTTP não aguarda requisições ativas — P2

Em `internal/webui/server.go:73`, uma goroutine chama `server.Shutdown`, mas o resultado não é aguardado nem propagado. `server.Serve` retorna assim que o servidor fecha seus listeners; isso pode acontecer enquanto `Shutdown` ainda aguarda handlers. `ServeControlled` então retorna para a CLI, que pode fechar o controlador e o banco antes das requisições terminarem. Um cadastro ou controle em andamento durante Ctrl+C pode perder a oportunidade de concluir normalmente.

Correção indicada: aguardar explicitamente o resultado do encerramento antes de liberar dependências. Ao atingir o timeout, fechar conexões restantes e definir claramente como aguardar ou interromper os handlers. Testar com uma requisição bloqueada que só termina depois do sinal de encerramento.

### 3. Leitura do modelo não observa cancelamento — P2

Em `internal/transcription/local.go:82`, o hash do modelo é calculado com `io.Copy` antes do timeout da consulta de versão. Esse trecho não observa o contexto. Cancelar a validação de ajustes ou encerrar o serviço durante essa leitura não interrompe o cálculo; `Controller.Close` aguarda as operações e goroutines envolvidas. O atraso depende do tamanho do modelo e da velocidade do armazenamento.

Correção indicada: verificar o contexto antes da leitura e entre blocos, com teste usando um leitor controlado. Isso melhora cancelamento cooperativo; não deve ser apresentado como garantia de interromper uma chamada ao sistema já bloqueada em leitura.

### 4. Admissão de operações e filas não tem capacidade definida — P2

`internal/service/operations.go:47` cria uma goroutine por operação distinta aceita. `internal/service/queue.go:28` acrescenta cada espera a um slice sem capacidade máxima. A deduplicação protege solicitações iguais, mas não várias fontes ou conversas diferentes. Um worker único limita execução simultânea, sem limitar goroutines e solicitações aguardando.

Correção indicada: definir capacidade de operações e esperas antes de persistir a aceitação, com resposta explícita quando a fila estiver cheia. Manter deduplicação, prioridade manual e cancelamento. Testar o limite com fontes distintas e verificar que recusar trabalho não deixa uma operação órfã no banco.

### 5. Exportação completa cresce em memória com o grupo — P3

`internal/archive/store.go:616` carrega todas as mensagens da conversa em um slice. `internal/archive/markdown.go:210` obtém esse snapshot e renderiza todo o arquivo em memória antes da escrita atômica. O programa mantém simultaneamente mensagens decodificadas e conteúdo Markdown; a leitura também ocupa a conexão e a transação do armazenamento. O custo aumenta conforme cresce o histórico, embora os lotes para IA já tenham limites próprios.

Correção indicada: medir heap e duração com grupos grandes e então considerar leitura e renderização por streaming sobre um snapshot consistente, gravando no arquivo temporário. Preservar ordenação, atomicidade e marcação da geração exportada; paginar sem uma visão consistente não resolve corretamente esse fluxo.

## Aspectos adequados

- Inicialização explícita, sem funções `init()` próprias que abram conexões ou iniciem processamento.
- Interfaces pequenas para transporte e motor, com implementações reais e simuladas.
- `Controller` compartilhado pela CLI e HTTP, com módulos privados que concentram estado de operações, conexão, histórico, áudio e exportação.
- Modelo de mensagem independente do protocolo; a integração depende do arquivo, sem dependência inversa do whatsmeow no modelo.
- Assets incorporados por `go:embed`; execução do painel sem Node.js.
- Transações, fontes estáveis, revisões e escrita atômica dos arquivos gerados.
- Download por arquivo, limites de mídia e saída JSON, timeouts de áudio e histórico, e limpeza de temporários.
- Cancelamento nas filas e prioridade para trabalho manual já estão presentes; faltam a capacidade de admissão e a proteção das transições terminais.

## Recomendações de desenho

A organização modular atual é proporcional ao produto local. Mantenha essa estrutura e concentre a próxima rodada no ciclo de vida e nos limites, antes de criar novas camadas.

O struct `Options` permite configuração explícita e injeção para os testes. Não há evidência suficiente de que substituí-lo por functional options traga benefício agora. Validação centralizada no construtor e compatibilidade entre opções são mais relevantes que o formato escolhido.

Estados de operações, etapas e tipos de mensagem ainda são strings livres. Tipos nomeados com constantes e métodos de transição podem reduzir combinações inválidas, preservando os valores JSON e SQLite existentes. Essa mudança complementa a correção do primeiro achado; o tipo nomeado, sozinho, não impede transições incorretas.

Compile-time assertions nos adaptadores reais podem documentar os contratos de transporte e motor. Não há necessidade identificada de um contêiner de DI ou abstrações genéricas adicionais.

## Validação e limites da revisão

Compilação, `go vet`, testes Go com detector de corrida e seis testes JavaScript passaram antes da avaliação. O teste exploratório de cancelamento reproduziu uma condição que a suíte existente não cobre. Não houve benchmark de grupos grandes; o quinto achado descreve o crescimento observado na implementação, sem estimativa inventada de volume ou memória.

Ordem sugerida: transições de operação → encerramento HTTP → cancelamento da validação do modelo → capacidade das filas → medição e streaming de exportação.
