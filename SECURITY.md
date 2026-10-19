# Segurança

O wpp armazena dados privados de uma conta WhatsApp. A interface HTTP é local (loopback), com proteções de Host, cookie e origem; ela não oferece acesso remoto autenticado. Não exponha o painel por proxy/túnel como se houvesse controle de acesso remoto.

## Relatar vulnerabilidades

O responsável é [Flávio Milan (@flaviomilan)](https://github.com/flaviomilan), mantenedor de [flaviomilan/wpp](https://github.com/flaviomilan/wpp). O canal escolhido é o **Private vulnerability reporting do GitHub**, sem necessidade de e-mail pessoal publicado.

Abra [Security → Report a vulnerability](https://github.com/flaviomilan/wpp/security/advisories/new) para relatar de forma privada ao mantenedor, quando o recurso estiver habilitado. Descreva versão, sistema, reprodução mínima com conteúdo fictício e impacto observado. **Não anexe bancos, sessões, QR, tokens, mensagens ou exports reais. Não abra uma issue pública com detalhes exploráveis.**

Se o formulário não estiver disponível, abra somente uma [issue solicitando ativação do canal privado](https://github.com/flaviomilan/wpp/issues/new), sem descrever a vulnerabilidade nem fornecer dados sensíveis. Aguarde a ativação para enviar os detalhes. Esse pedido público não é o relato privado.

## Preparação do repositório

O repositório permanece privado. Antes de anunciar o projeto publicamente, o mantenedor deve ativar **Private vulnerability reporting** em **Settings → Advanced Security**, se disponível, e testar o formulário com outra conta. A existência deste documento não comprova que o canal esteja ativo.

[Como relatar privadamente](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately) e [como habilitar o recurso](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/configure-for-a-repository).

Apenas a versão atual é alvo de manutenção durante esta preparação. Não há prazo de resposta ou histórico de releases públicos anunciado. Quando houver versões públicas, esta política deve identificar as versões atendidas.

## Dados e dependências

Mantenha banco/sessão e exports fora do repositório e faça backup com o processo parado. Logs e screenshots também podem conter dados privados. Use diretórios fictícios isolados para testes. Confira as licenças/avisos de dependências antes de redistribuir binários, motores de transcrição ou modelos.

## Barreiras de publicação

A lista positiva `release-files.txt`, as aprovações por hash e o validador de publicação são compartilhados por hooks, CI e empacotamento. O `pre-commit` lê os blobs do índice; o `pre-push` verifica todos os commits alcançáveis pelas referências enviadas, seus metadados e mensagens de tags anotadas. Clones rasos são recusados, e `git replace` não pode substituir os objetos examinados. Não basta apagar um segredo no commit seguinte. Autores, committers e taggers devem usar endereços GitHub `noreply`.

O Gitleaks é fixado em versão e checksums, mantém suas regras padrão, verifica também conteúdo decodificado e não aceita comentários de supressão. Ferramenta indisponível, versão incorreta ou erro de execução bloqueiam a publicação. Os candidatos são examinados em diretório temporário restrito, e seus conteúdos e achados não são impressos nem enviados como artefatos da CI. O pacote de fontes usa os mesmos bytes validados, sem reler arquivos depois da auditoria.

Nenhum scanner reconhece todos os nomes, telefones, mensagens ou outros dados pessoais que alguém possa inserir em um arquivo de código permitido. A revisão humana continua necessária. Hooks podem ser contornados e a CI executa após o push; portanto, essas medidas não garantem impedir qualquer vazamento antes que alcance o GitHub. Não dependemos de recursos pagos de secret protection. Esta proteção da publicação não substitui uma auditoria da aplicação em execução.

## Se ocorrer exposição

1. Interrompa pushes e distribuições. Não copie o valor encontrado para issues, logs, comentários ou relatórios públicos.
2. Revogue ou rotacione credenciais afetadas antes de limpar o histórico. Para dados pessoais, avalie quem teve acesso e o procedimento de incidente aplicável.
3. Remova o material de todas as referências afetadas e verifique um clone novo. Substituir o histórico altera hashes: colaboradores devem clonar novamente para evitar reintroduzir commits antigos.
4. Cópias locais, forks, artefatos, referências de pull requests e caches podem sobreviver à reescrita. Quando houver dados sensíveis já publicados, siga a [orientação do GitHub para remoção](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository), inclusive contato privado com o suporte quando necessário. Não envie dados ao suporte automaticamente.
