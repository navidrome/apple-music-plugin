SHELL := /usr/bin/env bash
.PHONY: test build package clean

PLUGIN_NAME := apple-music
WASM_FILE := plugin.wasm
TINYGO := $(shell command -v tinygo 2> /dev/null)

test:
	go test -race ./...
.PHONY: test

build:
ifdef TINYGO
	tinygo build -opt=2 -scheduler=none -no-debug -o $(WASM_FILE) -target wasip1 -buildmode=c-shared .
else
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(WASM_FILE) .
endif
.PHONY: build

package: build
	zip $(PLUGIN_NAME).ndp $(WASM_FILE) manifest.json
.PHONY: package

clean:
	rm -f $(WASM_FILE) $(PLUGIN_NAME).ndp
.PHONY: clean

release:
	@if [[ ! "$${V}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$$ ]]; then echo "Usage: make release V=X.X.X [PRE=true]"; exit 1; fi
	gh workflow run create-release.yml -f version=$${V} -f prerelease=$$(if [ "$(PRE)" = "true" ]; then echo true; else echo false; fi)
	@echo "Release v$${V}$$(if [ "$(PRE)" = "true" ]; then echo -prerelease; fi) workflow triggered. Check progress: gh run list --workflow=create-release.yml"
.PHONY: release
