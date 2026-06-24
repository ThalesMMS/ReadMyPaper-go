APP := readmypaper
CMD := ./cmd/readmypaper

.PHONY: run build test test-ci vet fmt check clean package package-macos

run:
	go run $(CMD)

build:
	mkdir -p bin
	go build -trimpath -o bin/$(APP) $(CMD)

test:
	go test ./...

test-ci:
	go test -tags ci ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

check: fmt test-ci
	go vet -tags ci ./...
	python3 -m py_compile internal/tts/bridge/*.py

clean:
	rm -rf bin dist

package:
	fyne package

package-macos:
	scripts/package-macos.sh
