.PHONY: build run test vet fmt clean hooks

build: hooks ## compile ./bin/nemo
	go build -o bin/nemo .

run: hooks  ## go run main.go
	go run .

test: hooks ## run tests
	go test -cover ./...

vet:        ## static checks
	go vet ./...

fmt:        ## gofmt all files
	gofmt -l -w .

clean:      ## remove build artifacts
	rm -rf bin

hooks:      ## install versioned git hooks (gofmt on pre-commit)
	git config core.hooksPath .githooks
