.PHONY: build run test vet crossbuild fmt sbom clean hooks

SBOM_FILE := sbom.cdx.json
CYCLONEDX_GOMOD := github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.12.0

build: hooks ## compile ./bin/nemo
	go build -o bin/nemo .

run: hooks  ## go run main.go
	go run .

test: hooks ## run tests
	go test -cover ./...

vet:        ## static checks
	go vet ./...

crossbuild: ## confirm the non-darwin build-tag stubs stay clean
	GOOS=linux GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...

fmt:        ## gofmt all files
	gofmt -l -w .

sbom:       ## generate CycloneDX SBOM (sbom.cdx.json)
	go run $(CYCLONEDX_GOMOD) mod -json -licenses -output $(SBOM_FILE) .

clean:      ## remove build artifacts
	rm -rf bin
	rm -f $(SBOM_FILE)

hooks:      ## install versioned git hooks (gofmt on pre-commit)
	git config core.hooksPath .githooks
