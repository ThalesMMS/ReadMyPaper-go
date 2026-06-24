# Arquitetura

## Objetivos

A implementação separa UI, estado, processamento documental e integrações externas. Isso permite testar o pipeline sem abrir uma janela Fyne, substituir o extrator ou o TTS por doubles e manter o processamento pesado fora da thread gráfica.

## Fluxo de um job

```text
Fyne UI
  │ valida PDF/opções
  ▼
Store.Create ──► cópia imutável para uploads/<id>/source.pdf
  │
  ▼
Manager (semáforo de workers + contexto de cancelamento)
  │
  ▼
ReadMyPaperPipeline
  ├─ NativeExtractor
  ├─ RepairReadingOrder
  ├─ FilterByLayout
  ├─ ScientificTextCleaner
  ├─ LLM Client opcional
  ├─ ScientificTextCleaner novamente
  ├─ cleaned_text.txt + metadata.json atômicos
  └─ Piper/Kokoro ──► reading.wav
  │
  ▼
Store.Update ──► UI observa versão e atualiza lista/detalhe
```

## Extração de PDF

`internal/pdfextract` usa o pacote `github.com/ledongthuc/pdf` para obter glifos posicionados. O extrator:

1. lê `CropBox`/`MediaBox`, com fallback pelas coordenadas observadas;
2. agrupa glifos por baseline;
3. reconstrói espaços com base na distância horizontal;
4. agrupa linhas próximas em blocos;
5. infere rótulos como título, cabeçalho de seção, legenda, lista, fórmula e texto;
6. registra regiões gráficas retangulares conservadoras e regiões de legendas.

A representação intermediária (`domain.ExtractedBlock`) não depende da biblioteca PDF, o que mantém as etapas seguintes testáveis.

## Ordem de leitura

`RepairReadingOrder` trabalha página a página. Blocos full-width são tratados como âncoras; o restante é analisado pela distribuição horizontal dos centros. Quando há evidência suficiente de duas colunas, os blocos são ordenados por coluna e por posição vertical, preservando títulos e elementos de largura ampla em sua posição relativa.

## Limpeza

A limpeza combina:

- rótulos estruturais;
- seções de corpo aceitas;
- seções de metadados e end matter descartáveis;
- padrões de DOI, ORCID, datas editoriais, afiliações, e-mails e licenças;
- eliminação opcional de citações numéricas;
- filtro espacial contra regiões de figura/tabela/legenda;
- normalização de hifens de quebra de linha, espaços e parágrafos.

O texto limpo é a fonte de verdade para detecção de idioma e TTS.

## LLM opcional

`internal/llm` envia JSON estruturado para um endpoint OpenAI-compatible. Os lotes respeitam página, quantidade de blocos e limite de caracteres. O cliente:

- usa temperatura zero;
- limita a resposta;
- não segue redirects;
- normaliza decisões desconhecidas para `KEEP`;
- força a preservação de cabeçalhos estruturais conhecidos;
- mantém intacto qualquer lote com falha.

O LLM não reescreve conteúdo no pipeline Go.

## TTS

A camada Go seleciona voz, baixa modelos Piper de forma atômica, prepara chunks, acompanha progresso e valida o WAV final. Os bridges Python são incorporados com `go:embed`, materializados no cache somente quando necessários e recebem requisições por JSON temporário.

Piper é o backend padrão. Kokoro é selecionável e, caso falhe, o pipeline remove o artefato parcial e tenta Piper.

## Concorrência e ciclo de vida

- `Store` usa `sync.RWMutex` e devolve cópias profundas.
- `Manager` limita concorrência por canal semáforo.
- um contexto comum cancela jobs na saída do aplicativo;
- a UI consulta apenas o contador de versão do store a cada 500 ms;
- chamadas que alteram widgets voltam à thread Fyne com `fyne.Do`;
- o close intercept é idempotente e removido antes de fechar a janela.

## Persistência

Cada job completo contém:

```text
uploads/<id>/source.pdf
outputs/<id>/cleaned_text.txt
outputs/<id>/reading.wav
outputs/<id>/metadata.json
```

Na inicialização, apenas jobs com WAV válido em tamanho mínimo e metadados coerentes são restaurados. Caminhos vindos do JSON são validados contra a raiz de uploads. Jobs incompletos não aparecem como concluídos.

## Pontos de extensão

- implementar outro `pdfextract.Extractor`;
- adicionar `tts.Engine`;
- incluir vozes em `internal/tts/catalog.go`;
- substituir o `jobs.Processor` em integrações/testes;
- evoluir o classificador de layout sem modificar UI ou persistência.
