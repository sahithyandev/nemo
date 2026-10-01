.PHONY: build run test vet crossbuild fmt clean hooks release-package linux-acceptance

build: hooks ## compile ./bin/nemo
	go build -o bin/nemo .

run: hooks  ## go run main.go
	go run .

test: hooks ## run tests
	go test -cover ./...

vet:        ## static checks
	go vet ./...

crossbuild: ## compile all supported release platforms
	GOOS=linux GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...

fmt:        ## gofmt all files
	gofmt -l -w .

clean:      ## remove build artifacts
	rm -rf bin

hooks:      ## install versioned git hooks (gofmt on pre-commit)
	git config core.hooksPath .githooks

release-package: ## package release candidates into a new OUTPUT directory
	python3 scripts/package-release.py --output "$(OUTPUT)"

linux-acceptance: ## exercise only disposable Linux fixtures
	python3 scripts/linux-acceptance.py
