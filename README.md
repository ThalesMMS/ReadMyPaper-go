# ReadMyPaper — Go/Fyne

Aplicativo desktop local para transformar artigos científicos em PDF em texto limpo e áudio WAV. Esta implementação porta o fluxo do **ReadMyPaper-py** para **Go**, com interface nativa em **Fyne** e organização modular para extração, limpeza, persistência, revisão opcional por LLM e síntese de voz.

## Funcionalidades

- seleção de PDF por interface desktop;
- validação de assinatura, tamanho e limite de páginas;
- extração local de texto e coordenadas do PDF em Go;
- reparo heurístico da ordem de leitura em artigos com múltiplas colunas;
- filtragem espacial de conteúdo associado a figuras, tabelas e legendas;
- limpeza orientada a artigos científicos, incluindo front matter, referências, agradecimentos, apêndices e citações numéricas;
- preservação opcional de títulos e cabeçalhos;
- detecção automática de inglês ou português brasileiro;
- verbalização de notação científica para leitura natural;
- revisão opcional de blocos por endpoint OpenAI-compatible local;
- TTS local com **Piper** (rápido) ou **Kokoro** (qualidade), com fallback de Kokoro para Piper;
- fila concorrente de jobs, progresso, histórico persistente e restauração após reiniciar;
- reprodução local e exportação do WAV, texto limpo e PDF original;
- retenção opcional por TTL e exclusão segura dos artefatos de um job.

## Arquitetura

```text
cmd/readmypaper/          entrada do aplicativo Fyne
internal/app/             interface desktop e reprodução de WAV
internal/pdfextract/      extração posicionada de PDF em Go
internal/cleaner/         ordem de leitura, filtros e verbalização
internal/llm/             cliente OpenAI-compatible com fail-open por lote
internal/tts/             catálogo, download de vozes e backends Piper/Kokoro
internal/jobs/            store concorrente, execução e persistência
internal/pipeline/        orquestração do processamento
internal/config/          diretórios, limites e variáveis de ambiente
internal/domain/          tipos compartilhados
internal/util/            arquivos, URLs, IDs e validação WAV
```

O núcleo do aplicativo, a interface, o processamento de PDF, a limpeza e a orquestração são implementados em Go. Piper e Kokoro são acionados por pequenos bridges Python incorporados ao binário, pois os runtimes de referência desses modelos são distribuídos como pacotes Python. Nenhum servidor web é necessário.

Uma descrição técnica mais detalhada está em [ARCHITECTURE.md](ARCHITECTURE.md).

## Requisitos

### Comuns

- Go **1.24.1** ou mais recente;
- Python **3.10–3.12** para os backends TTS; Python 3.12 é a opção mais compatível;
- acesso à internet no primeiro uso de cada voz/modelo; depois os artefatos ficam em cache local.

### Linux (Debian/Ubuntu)

Instale as dependências de compilação do Fyne e, para Kokoro, `espeak-ng`:

```bash
sudo apt update
sudo apt install -y gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev espeak-ng
```

Para reprodução pelo botão **Play**, tenha ao menos um destes comandos: `ffplay`, `paplay` ou `aplay`.

### macOS

```bash
xcode-select --install
brew install espeak-ng
```

A reprodução usa `afplay`, já fornecido pelo macOS.

### Windows

Use Go com um compilador C compatível com Fyne, como o toolchain MinGW-w64 do MSYS2. Para Kokoro, instale também o eSpeak NG. A reprodução usa PowerShell e `System.Media.SoundPlayer`.

## Instalação dos backends TTS

Crie um ambiente virtual no diretório do projeto:

### Linux/macOS

```bash
python3.12 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -r requirements-tts.txt
```

### Windows PowerShell

```powershell
py -3.12 -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
python -m pip install -r requirements-tts.txt
```

O aplicativo procura `python3`, `python` ou `py -3`. Para fixar o interpretador do ambiente virtual:

```bash
export READMYPAPER_PYTHON_BIN="$PWD/.venv/bin/python"
```

No PowerShell:

```powershell
$env:READMYPAPER_PYTHON_BIN = "$PWD\.venv\Scripts\python.exe"
```

## Executar

```bash
go run ./cmd/readmypaper
```

Consultar a versão sem abrir a interface:

```bash
go run ./cmd/readmypaper --version
```

## Compilar

```bash
mkdir -p bin
go build -trimpath -o bin/readmypaper ./cmd/readmypaper
```

No Windows, use `bin/readmypaper.exe` como destino. Para empacotar com os metadados de `FyneApp.toml`:

```bash
go install fyne.io/tools/cmd/fyne@latest
fyne package
```

## Uso

1. Abra **New job** e selecione um PDF.
2. Escolha idioma, voz, engine e velocidade.
3. Ajuste as regras de limpeza.
4. Opcionalmente habilite um endpoint LLM local compatível com `/chat/completions`.
5. Clique em **Process PDF**.
6. Acompanhe o job na aba **Jobs** e salve ou reproduza os resultados.

Piper baixa apenas a voz escolhida no primeiro uso. Kokoro administra seu próprio cache de modelo. Quando Kokoro falha, o pipeline tenta Piper automaticamente e informa o engine efetivamente usado.

## Endpoint LLM opcional

A URL pode ser informada como `127.0.0.1:11434/v1` ou como URL HTTP(S) completa. O cliente acrescenta `/chat/completions`, rejeita credenciais embutidas, query string e fragmentos, limita o tamanho das respostas e não segue redirecionamentos.

O modelo recebe blocos de texto e metadados de layout em lotes delimitados. A saída só pode manter, remover ou reordenar blocos; reescrita textual é deliberadamente ignorada. Em erro de rede ou resposta inválida, o lote é mantido sem alteração.

## Diretórios de dados

| Plataforma | Dados | Cache |
| --- | --- | --- |
| Linux | `~/.local/share/ReadMyPaper` | `~/.cache/ReadMyPaper` |
| macOS | `~/Library/Application Support/ReadMyPaper` | `~/Library/Caches/ReadMyPaper` |
| Windows | `%LOCALAPPDATA%\ReadMyPaper` | `%LOCALAPPDATA%\ReadMyPaper\Cache` |

Os PDFs copiados ficam em `uploads/<job-id>`. Texto, áudio e `metadata.json` ficam em `outputs/<job-id>`. Quando o TTL está habilitado, a inicialização também remove diretórios órfãos antigos deixados por jobs interrompidos ou metadados inválidos.

## Configuração por ambiente

| Variável | Padrão | Finalidade |
| --- | --- | --- |
| `READMYPAPER_DATA_DIR` | diretório de dados da plataforma | PDFs e resultados |
| `READMYPAPER_CACHE_DIR` | diretório de cache da plataforma | vozes, modelos e bridges |
| `READMYPAPER_MAX_WORKERS` | `2` | jobs processados simultaneamente |
| `READMYPAPER_MAX_UPLOAD_BYTES` | `52428800` | tamanho máximo do PDF |
| `READMYPAPER_MAX_PDF_PAGES` | `200` | número máximo de páginas |
| `READMYPAPER_SPEECH_RATE_MIN` | `0.5` | velocidade mínima da UI/API interna |
| `READMYPAPER_SPEECH_RATE_MAX` | `2.0` | velocidade máxima da UI/API interna |
| `READMYPAPER_MAX_PENDING_JOBS` | `10` | jobs pendentes ou em execução |
| `READMYPAPER_JOB_RETENTION_HOURS` | `0` | TTL aplicado na inicialização; `0` desabilita |
| `READMYPAPER_LLM_URL` | vazio | URL OpenAI-compatible padrão |
| `READMYPAPER_LLM_MODEL` | vazio | modelo enviado ao endpoint |
| `READMYPAPER_LLM_ENABLED` | `false` | inicia a opção LLM marcada |
| `READMYPAPER_LLM_API_KEY` | `apikey` | bearer token do endpoint local |
| `READMYPAPER_PYTHON_BIN` | autodetectado | interpretador dos bridges TTS |

Valores numéricos inválidos usam o padrão, exceto limites estruturalmente impossíveis, que impedem a inicialização com mensagem explícita.

## Testes e qualidade

```bash
go test ./...
go vet ./...
```

Em ambientes sem OpenGL/X11, como CI headless:

```bash
go test -tags ci ./...
go build -tags ci -o bin/readmypaper-ci ./cmd/readmypaper
```

Os testes cobrem ordem multicoluna, filtro espacial, limpeza científica, verbalização, extração PDF, LLM, catálogo de vozes, persistência, store concorrente, pipeline e construção da interface Fyne.

## Privacidade e segurança

- não há telemetria;
- PDFs e resultados permanecem nos diretórios locais do aplicativo;
- a rede é usada para baixar modelos/vozes e apenas quando o LLM opcional está habilitado;
- deleções são restritas aos diretórios de dados e a identificadores validados;
- caminhos restaurados de metadados são aceitos somente dentro da raiz de uploads;
- o conteúdo do PDF não é passado em argumentos de shell: os bridges recebem um arquivo JSON temporário e são executados sem shell.

Consulte [SECURITY.md](SECURITY.md) para o modelo de confiança.

## Limitações conhecidas

- PDFs compostos apenas por imagens não passam por OCR; o aplicativo informa que o documento requer OCR.
- A extração nativa usa heurísticas de glifos, fontes e geometria. PDFs com encoding não padrão, layouts muito ornamentados ou tabelas sem contorno podem exigir ajustes.
- O botão de reprodução depende de um player WAV já disponível no sistema.
- O primeiro uso pode ser demorado devido ao download e inicialização dos modelos TTS.

## Licença

GPL-3.0-or-later, em continuidade com o projeto Python de referência. Consulte [LICENSE](LICENSE).
