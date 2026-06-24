# Plano de Empacotamento Autocontido — ReadMyPaper-go

## Objetivo

Disponibilizar o `ReadMyPaper-go` como um aplicativo `.app` para macOS que abre com dois cliques e não depende de Python, variáveis de ambiente ou terminal. O bundle deve conter o binário Go, a interface Fyne, o runtime Python e os pacotes necessários para os backends de TTS (Piper e Kokoro).

## Estado atual

- O núcleo do aplicativo é Go/Fyne.
- Os scripts Python dos backends TTS são embutidos no binário via `go:embed` e extraídos em cache no primeiro uso.
- Em runtime, o app executa esses scripts usando um interpretador Python externo configurado por `READMYPAPER_PYTHON_BIN`.
- O ambiente virtual deve ser criado manualmente e ativado antes de rodar o app.
- O arquivo `requirements-tts.txt` lista as dependências Python.
- O target `fyne package` existe no `Makefile`, mas gera apenas o binário Go com metadados; não leva o Python embarcado.

## Decisões de arquitetura

### 1. Python relocável

Usar como base o **Python standalone** do projeto `astral-sh/python-build-standalone` (continuação do `indygreg/python-build-standalone`). Essa distribuição já é compilada para ser relocável no macOS e funciona quando copiada para dentro de um `.app`.

Pacotes a instalar no Python embarcado:

- `piper-tts`
- `kokoro-onnx` (ou equivalente usado no projeto)
- quaisquer outras dependências listadas em `requirements-tts.txt`

A instalação deve ser feita diretamente no Python relocável, sem criar `venv` interno, para reduzir complexidade e caminhos absolutos.

O script `scripts/package-macos.sh` usa CPython 3.12 por padrão e permite trocar a origem com `PYTHON_STANDALONE_REPO`, `PYTHON_VERSION_PREFIX` ou URLs explícitas por arquitetura.

### 2. Estrutura do `.app`

```text
ReadMyPaper.app/
├── Contents/
│   ├── Info.plist
│   ├── MacOS/
│   │   └── readmypaper
│   └── Resources/
│       ├── Icon.png
│       └── python/
│           ├── bin/python3
│           └── lib/python3.12/site-packages/
│               ├── piper/
│               ├── kokoro/
│               └── ...
```

### 3. Descoberta de recursos em runtime

Adicionar uma função Go que, a partir de `os.Executable()`, detecta se o binário está dentro de um `.app` e resolve o caminho relativo para `Contents/Resources/python/bin/python3`.

A ordem de resolução do interpretador deve ser:

1. `READMYPAPER_PYTHON_BIN` se estiver definida (modo desenvolvimento).
2. Python bundled em `Contents/Resources/python/bin/python3`.
3. Python bundled específico da arquitetura em `Contents/Resources/python-arm64/bin/python3` ou `Contents/Resources/python-x86_64/bin/python3`, usado pelo pacote universal.
4. `python3` do `PATH` (fallback para máquinas de desenvolvimento).

### 4. Cache de vozes e modelos

Vozes e modelos TTS continuarão sendo baixados na primeira execução e armazenados em `~/Library/Caches/ReadMyPaper`. Isso mantém o bundle menor e permite atualizar modelos sem rebuild.

## Mudanças necessárias

### Código Go

- `internal/config/config.go`
  - Adicionar função auxiliar para resolver caminho do Python embarcado.
  - Preservar leitura de `READMYPAPER_PYTHON_BIN`.

- `internal/tts/tts.go`
  - Ajustar detecção do interpretador para usar a função de resolução.

- `internal/tts/piper.go` e `internal/tts/kokoro.go`
  - Garantir que os bridges Python materializados em cache sejam executados com o Python embarcado.

### Build e scripts

- Criar `scripts/package-macos.sh`.
  - Baixar Python standalone para a arquitetura alvo (arm64, x86_64 ou universal).
  - Instalar dependências de `requirements-tts.txt`.
  - Compilar `cmd/readmypaper` com `go build`.
  - Montar a estrutura do `.app`.
  - Gerar `Info.plist`.
  - Copiar ícone de `assets/Icon.png`.
  - Opcional: assinar com `codesign` e notarizar com `notarytool` (se houver certificado de desenvolvedor).

- Atualizar `Makefile`.
  - Adicionar target `package-macos`.
  - Manter target `package` do Fyne como alternativa leve.

## Fases de execução

### Fase 1 — Análise do código

- Mapear todos os pontos onde `READMYPAPER_PYTHON_BIN` e `python3` são usados.
- Confirmar quais pacotes Python estão em `requirements-tts.txt`.
- Verificar se Kokoro exige binários adicionais ou apenas pacotes Python.

### Fase 2 — Protótipo de Python relocável

- Baixar `python-build-standalone` para macOS.
- Instalar `requirements-tts.txt`.
- Executar os bridges Python manualmente para validar Piper e Kokoro.
- Medir tamanho do Python após instalação.

### Fase 3 — Detecção de recursos em Go

- Implementar função de resolução de caminho do Python embarcado.
- Adicionar testes unitários para os cenários:
  - dentro do `.app`
  - fora do `.app`
  - com variável de ambiente definida

### Fase 4 — Script de empacotamento

- Criar `scripts/package-macos.sh`.
- Suportar arquitetura arm64 e x86_64.
- Gerar `Info.plist` com bundle ID `io.github.thalesmms.readmypaper` (conforme `FyneApp.toml`).
- Verificar se o ícone existe em `assets/Icon.png`.

### Fase 5 — Testes

- Executar o `.app` em uma máquina sem Python no `PATH`.
- Processar o PDF de exemplo.
- Confirmar que o áudio `reading.wav` é gerado.
- Testar com Piper e Kokoro.
- Testar fallback quando a variável `READMYPAPER_PYTHON_BIN` é definida.

### Fase 6 — Documentação

- Atualizar `README.md` com instruções de build do `.app`.
- Registrar tamanho do bundle, tempo de primeira execução e limitações de notarização.

## Tamanho esperado do bundle

- Build arm64 validado em 2026-06-24: `dist/ReadMyPaper.app` ficou com ~854 MB.
- O maior custo vem do Kokoro e suas dependências atuais, incluindo Torch, spaCy, Transformers e rodas nativas.
- Builds apenas com Piper ficariam substancialmente menores, mas não atendem ao objetivo deste pacote autocontido com Piper e Kokoro.

## Riscos e limitações

- **Notarização**: sem Apple Developer ID, o usuário precisa autorizar o app em `Segurança e Privacidade`.
- **Primeira execução**: ainda pode ser lenta devido ao download de vozes e modelos.
- **Tamanho**: Kokoro e modelos aumentam significativamente o bundle se incluídos.
- **Atualizações**: mudanças nos pacotes Python exigem novo build.

## Próximos passos recomendados

1. Iniciar pela Fase 2 (protótipo de Python relocável).
2. Depois de validado, aplicar a Fase 3 (detecção em Go) e Fase 4 (script de empacotamento).
