# Architecture

## Goals

The implementation separates UI, state, document processing, and external integrations. This makes it possible to test the pipeline without opening a Fyne window, replace the extractor or TTS with test doubles, and keep heavy processing outside the graphical thread.

## Job Flow

```text
Fyne UI
  │ validates PDF/options
  ▼
Store.Create ──► immutable copy to uploads/<id>/source.pdf
  │
  ▼
Manager (worker semaphore + cancellation context)
  │
  ▼
ReadMyPaperPipeline
  ├─ NativeExtractor
  ├─ RepairReadingOrder
  ├─ FilterByLayout
  ├─ ScientificTextCleaner
  ├─ optional LLM Client
  ├─ ScientificTextCleaner again
  ├─ atomic cleaned_text.txt + metadata.json
  └─ Piper/Kokoro ──► reading.wav
  │
  ▼
Store.Update ──► UI observes version and updates list/detail
```

## PDF Extraction

`internal/pdfextract` uses `github.com/ledongthuc/pdf` to obtain positioned glyphs. The extractor:

1. reads `CropBox`/`MediaBox`, with fallback to observed coordinates;
2. groups glyphs by baseline;
3. reconstructs spaces from horizontal distance;
4. groups nearby lines into blocks;
5. infers labels such as title, section heading, caption, list, formula, and text;
6. records conservative rectangular graphic regions and caption regions.

The intermediate representation (`domain.ExtractedBlock`) does not depend on the PDF library, which keeps the following stages testable.

## Reading Order

`RepairReadingOrder` works page by page. Full-width blocks are treated as anchors; the remaining blocks are analyzed through the horizontal distribution of their centers. When there is enough evidence for two columns, blocks are ordered by column and vertical position while preserving titles and wide elements in their relative positions.

## Cleaning

Cleaning combines:

- structural labels;
- accepted body sections;
- disposable metadata and end-matter sections;
- DOI, ORCID, editorial date, affiliation, email, and license patterns;
- optional removal of numeric citations;
- spatial filtering against figure/table/caption regions;
- normalization of line-break hyphens, spaces, and paragraphs.

The cleaned text is the source of truth for language detection and TTS.

## Optional LLM

`internal/llm` sends structured JSON to an OpenAI-compatible endpoint. Batches respect page, block-count, and character limits. The client:

- uses zero temperature;
- limits the response;
- does not follow redirects;
- normalizes unknown decisions to `KEEP`;
- forces known structural headings to be preserved;
- keeps any failed batch intact.

The LLM does not rewrite content in the Go pipeline.

## TTS

The Go layer selects the voice, downloads Piper models atomically, prepares chunks, tracks progress, and validates the final WAV. The Python bridges are embedded with `go:embed`, materialized in cache only when needed, and receive requests through temporary JSON.

Piper is the default backend. Kokoro is selectable and, if it fails, the pipeline removes the partial artifact and tries Piper.

## Concurrency And Lifecycle

- `Store` uses `sync.RWMutex` and returns deep copies.
- `Manager` limits concurrency with a semaphore channel.
- a shared context cancels jobs when the app exits;
- the UI polls only the store version counter every 500 ms;
- calls that mutate widgets return to the Fyne thread through `fyne.Do`;
- the close intercept is idempotent and removed before closing the window.

## Persistence

Each completed job contains:

```text
uploads/<id>/source.pdf
outputs/<id>/cleaned_text.txt
outputs/<id>/reading.wav
outputs/<id>/metadata.json
```

At startup, only jobs with a valid minimum-size WAV and coherent metadata are restored. Paths from JSON are validated against the uploads root. Incomplete jobs are not shown as completed.

## Extension Points

- implement another `pdfextract.Extractor`;
- add a `tts.Engine`;
- include voices in `internal/tts/catalog.go`;
- replace `jobs.Processor` in integrations/tests;
- evolve the layout classifier without changing UI or persistence.
