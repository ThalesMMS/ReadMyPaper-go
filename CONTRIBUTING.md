# Contribuindo

## Ambiente

1. Instale Go e as dependências nativas do Fyne.
2. Execute `go mod download`.
3. Opcionalmente configure o ambiente Python descrito no README para testes manuais de TTS.

## Antes de enviar mudanças

```bash
gofmt -w .
go test ./...
go vet ./...
```

Em runner headless:

```bash
go test -tags ci ./...
```

## Diretrizes

- preserve as fronteiras entre UI, domínio, pipeline e adaptadores;
- não bloqueie a thread Fyne com extração, rede ou TTS;
- use interfaces pequenas nas integrações pesadas;
- escreva arquivos finais de forma atômica;
- mantenha falhas do LLM como fail-open;
- inclua teste de regressão para mudanças em heurísticas de limpeza ou layout;
- não acrescente chamadas de telemetria.
