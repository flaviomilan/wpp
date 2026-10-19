# Transcrição local de áudios

O wpp mantém uma fila de metadados no SQLite. O áudio é baixado apenas durante o processamento; o resultado permanente é o texto, associado à mensagem original e ao seu ID de evidência.

## Preparar e executar

A instalação portátil está em `.tools/audio` e não modifica diretórios do sistema. O script instala whisper.cpp 1.8.7 e baixa um modelo multilíngue:

```bash
bash scripts/setup-audio.sh
```

O padrão é `large-v3-turbo`, aproximadamente 1,51 GiB. O script verifica tamanho e SHA-256 publicados pelo repositório do modelo. FFmpeg deve estar instalado. São necessários curl, tar, Python 3, make e um compilador C++; se faltar CMake em Linux x86_64/aarch64, o script instala uma cópia portátil.

Após compilar o wpp atualizado, pare a captura anterior com Ctrl+C e inicie:

```bash
./wpp \
  --whisper-bin ./.tools/audio/whisper/build/bin/whisper-cli \
  --whisper-model ./.tools/audio/models/ggml-large-v3-turbo.bin \
  run
```

As flags são globais e ficam antes de `run`. O controlador de `serve`/`run` usa preferências persistidas ou os caminhos da instalação portátil por padrão. `--whisper-model` junto das outras flags substitui os ajustes nessa execução e habilita transcrição. Se não houver motor válido, a fila permanece registrada e a captura continua. A instalação não inicia conexão com a conta automaticamente.

Consulte o progresso:

```bash
./wpp audio status
./wpp audio retry S-ID-DA-FONTE
./wpp serve
```

O caminho recomendado é `./wpp serve`, sem `run` paralelo. O painel controla captura/transcrição e tem a tela **Áudios**, com filtros, estado, tentativas, etapas e erros. Modelo e opções avançadas validam o motor antes de salvar; mudanças afetam os próximos áudios. Pausar transcrição conclui o áudio atual. Sincronização pontual processa uma lista finita da conversa mesmo com captura pausada. **Tentar novamente** pode antecipar a próxima tentativa após um erro, inclusive antes das três falhas. Na interface proprietária, executa somente esse áudio e respeita a pausa global da transcrição. Na CLI ou no painel em conflito, apenas reenfileira. A tentativa explícita reinicia o orçamento de tentativas. A fila mostra o texto transcrito em cartões, com detalhe integral e cópia. A transcrição aparece na mensagem em **Conversas**, pode ser encontrada pela busca textual e entra no JSONL completo e nas novas revisões dos lotes de IA.

## Qualidade e configuração

O motor executa whisper.cpp localmente, com modelo multilíngue, português por padrão, beam search de tamanho 5 e fallback do próprio motor habilitado. O processamento preserva a fala: converte para WAV PCM de 16 bits, mono e 16 kHz, sem traduzir nem aplicar filtros de redução de ruído.

O modelo `large-v3-turbo` é a opção inicial instalada. Para comparar com `large-v3`, considerando espaço e tempo de processamento, instale esse modelo em um diretório com capacidade suficiente:

```bash
bash scripts/setup-audio.sh large-v3 .tools/audio-large
```

O script aceita também `medium`. Modelos `.en` não são apropriados para português. A escolha do modelo não garante fidelidade em ruído, nomes próprios ou fala sobreposta; valide exemplos reais do grupo antes de tratar prazos, valores e decisões como confirmados.

| Opção | Padrão | Efeito |
|---|---|---|
| `--whisper-model` | vazio | Caminho do modelo; habilita processamento |
| `--whisper-bin` | `whisper-cli` | Executável do motor |
| `--ffmpeg-bin` | `ffmpeg` | Executável de conversão |
| `--audio-language` | `pt` | Idioma; `auto` detecta |
| `--audio-threads` | `0` | Até 8 CPUs disponíveis; aceita 1 a 256 |
| `--audio-timeout` | `30m` | Tempo máximo por tarefa |
| `--audio-vocabulary` | vazio | Nomes e termos específicos, até 2048 bytes |

Exemplo de vocabulário: `--audio-vocabulary "Aurora, Mariana, homologação, DEM-023"`. Esse texto orienta o reconhecimento; não é uma instrução para resumir ou criar decisões. A transcrição é literal segundo o motor, sem uma etapa posterior de reescrita por modelo de linguagem.

Cada resultado registra texto, segmentos com início/fim em milissegundos, idioma, nome e SHA-256 do modelo, versão informada pelo motor e horário UTC de conclusão. O painel mostra uma prévia de até 12.000 bytes; a versão integral permanece no banco e nas evidências/exportações.

## Captura e estados da fila

Somente mensagens efetivamente arquivadas das conversas acompanhadas entram na fila. A normalização extrai um descritor mínimo do áudio: caminho de mídia, chave de decriptação, hashes, MIME, tamanho e duração disponíveis. Não armazena o conteúdo sonoro, waveform ou texto de mensagens citadas nesse descritor.

Mensagem e tarefa são gravadas na mesma transação. Replays não duplicam tarefas. Os metadados ficam somente em `audio_jobs` e não entram nos snapshots, lotes, respostas da API ou no inspetor de tabelas. O diretório e banco continuam precisando da proteção local já usada para a sessão.

| Estado | Significado |
|---|---|
| `waiting_metadata` | Áudio conhecido, sem os dados necessários para baixar |
| `pending` | Metadados disponíveis, aguardando processamento |
| `processing` | Tarefa reservada pelo worker |
| `done` | Texto persistido; metadados privados de download descartados |
| `failed` | Três tentativas reais falharam; permite retry |
| `cancelled` | Mensagem excluída ou substituída antes da conclusão |

O worker é único por diretório, protegido por `audio-worker.lock`. Trabalha em série, fora das transações de captura, verificando novas tarefas a cada meio segundo. Se a conversa for desativada por `untrack`, suas tarefas pendentes aguardam uma futura reativação.

A reserva possui token e prazo; uma conclusão atrasada não pode substituir uma reserva nova. Após falhas, há espera crescente de dois e quatro minutos antes das próximas tentativas. Desconexão e encerramento normal não consomem o limite de tentativas.

Na reinicialização, com o lock adquirido, o worker remove resíduos temporários e devolve reservas interrompidas à fila imediatamente. A recuperação por expiração do prazo também impede reservas abandonadas de permanecerem ativas.

## Áudios temporários

Durante a tarefa, `data/tmp-audio/job-…/` contém o áudio baixado, o WAV convertido e o JSON de saída. O diretório tem permissão 0700 e o arquivo original, 0600. O worker remove o diretório após persistir o resultado ou terminar com falha/cancelamento.

Ctrl+C cancela os subprocessos e remove os arquivos temporários. Um encerramento forçado, falha de energia ou erro do sistema de arquivos pode deixar resíduos; a próxima execução do worker tenta limpá-los antes de retomar. A remoção normal não equivale a apagamento forense do armazenamento.

Há limite de 64 MiB no download, 256 MiB no WAV, 16 MiB no JSON do motor e 4 MiB no texto da transcrição. O download tem prazo de dois minutos e a tarefa usa o prazo configurado. Se um limite impedir processamento integral, a tarefa falha; não publica uma transcrição silenciosamente truncada.

## Persistência, revisões e IA

A versão do banco é 7. As migrações preservam a fila, etapas, configurações e operações do controlador, sem alterar IDs ou reescrever revisões históricas. A exportação usa exclusivamente o diário de eventos JSONL. Áudios antigos sem descritor continuam aguardando metadados.

Versões anteriores não guardavam os metadados de download. Quando o histórico entregar novamente um desses áudios com o descritor, a tarefa ficará pronta. O programa não garante recuperação de mídia expirada ou indisponível.

`CompleteAudio` grava a transcrição, uma mudança `update`, a geração de exportação e a conclusão da tarefa em uma transação. Não troca o tipo áudio nem escreve a transcrição sobre `Message.Text`.

Se um lote já foi confirmado, o texto chega como nova revisão em um próximo lote. Lotes pendentes e suas revisões conservam os bytes entregues. Os limites de entrada e a fragmentação continuam aplicáveis a transcrições extensas.

O agente deve reconhecer a indicação **transcrição automática**, considerar segmentos incertos e vincular as conclusões ao ID/revisão da fonte. A aplicação não transforma uma fala em aprovação de demanda automaticamente.

Exclusões recebidas durante processamento invalidam a reserva e impedem uma nova transcrição de ressuscitar a mensagem. Se o texto já havia sido publicado, suas revisões históricas permanecem arquivadas, conforme a política existente de evidências. Aliases preservam a associação à fonte e conciliam tarefas duplicadas.

## Validação

`make check` cobre fila transacional, replays, chegada tardia de metadados, migração, cancelamento, expiração de reservas, retry, exclusões, aliases, transcrição preservada na unificação, limpeza temporária, segredo dos metadados, integração com a busca e chegada em novo lote.

O teste real do motor é opt-in e usa um áudio de exemplo, sem conta WhatsApp:

```bash
WPP_TEST_MODEL="$PWD/.tools/audio/models/ggml-large-v3-turbo.bin" \
WPP_TEST_WHISPER="$PWD/.tools/audio/whisper/build/bin/whisper-cli" \
WPP_TEST_AUDIO="$PWD/.tools/audio/whisper/samples/jfk.wav" \
go test -v ./internal/transcription -run TestRealWhisperPipeline -count=1
```

Esse teste confirma execução, armazenamento, identificação do modelo e limpeza. A qualidade em áudios reais de português deve ser conferida com exemplos representativos das conversas.

Fontes técnicas: [whisper.cpp](https://github.com/ggml-org/whisper.cpp), [modelos do projeto](https://huggingface.co/ggerganov/whisper.cpp), [download de mídia no whatsmeow](https://pkg.go.dev/go.mau.fi/whatsmeow#Client.DownloadToFile).

## Localização no painel

Abra **Ajustes → Transcrição**, ou **Configurar transcrição** na área Áudios. Idioma e nomes/termos ficam visíveis; modelo, executáveis, threads e timeout ficam em **Modelo e opções avançadas**. Use **Salvar ajustes** para confirmar. Erros mantêm o rascunho e os valores persistidos anteriores; sair com alterações pede continuar editando ou descartar. A preferência de transcrição automática é um controle separado do formulário.
