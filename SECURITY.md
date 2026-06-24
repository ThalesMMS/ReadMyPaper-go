# Segurança

## Modelo de confiança

ReadMyPaper é um aplicativo desktop local. PDFs devem ser tratados como entrada não confiável, e endpoints LLM como serviços externos mesmo quando executados na máquina local.

## Controles implementados

- validação do cabeçalho `%PDF-` antes de criar o job;
- limites configuráveis de tamanho, páginas, workers e fila;
- IDs de job criptograficamente aleatórios;
- cópia do PDF para um diretório controlado antes do processamento;
- escrita atômica de texto, metadados, modelos e áudio temporário;
- validação de caminhos antes de restaurar ou excluir artefatos;
- execução dos bridges TTS por `exec.CommandContext`, sem shell;
- texto e opções trafegam por JSON temporário, não por argumentos de linha de comando;
- timeout, limite de resposta e bloqueio de redirects no cliente LLM;
- política fail-open do LLM para evitar perda silenciosa de conteúdo;
- cancelamento dos subprocessos TTS e jobs no encerramento.

## Dados enviados pela rede

- Piper: download do modelo e do JSON da voz selecionada.
- Kokoro: downloads administrados pelo pacote/model hub usado pelo runtime Kokoro.
- LLM: somente quando habilitado; os blocos sobreviventes da etapa espacial são enviados ao endpoint configurado para classificação e ordenação.

Não há telemetria.

## Recomendações

- prefira endpoint LLM em loopback ou rede confiável;
- use HTTPS e token específico quando o endpoint não estiver em loopback;
- não execute o aplicativo com privilégios administrativos;
- mantenha Go, Fyne, Python e pacotes TTS atualizados;
- processe PDFs sensíveis somente em ambiente cuja política de cache e backup seja conhecida.

## Relato de vulnerabilidade

Não publique documentos sensíveis, tokens ou modelos privados em um issue. Envie um relato mínimo reproduzível ao mantenedor do repositório, incluindo versão, sistema operacional, impacto e passos de reprodução sem dados clínicos ou pessoais.
