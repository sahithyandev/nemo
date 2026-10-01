.PHONY: build run test vet crossbuild fmt clean hooks

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

clean:      ## remove build artifacts
	rm -rf bin

hooks:      ## install versioned git hooks (gofmt on pre-commit)
	git config core.hooksPath .githooks
