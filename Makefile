.PHONY: build run test vet fmt clean

build:      ## compile ./bin/nemo
	go build -o bin/nemo .

run:        ## go run main.go
	go run .

test:       ## run tests
	go test ./...

vet:        ## static checks
	go vet ./...

fmt:        ## gofmt all files
	gofmt -l -w .

clean:      ## remove build artifacts
	rm -rf bin
