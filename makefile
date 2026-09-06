BINARY_NAME=zoraxy-geoheaders

all: build

build:
	go build -ldflags="-s -w" -o $(BINARY_NAME) .

test:
	go test -v ./...

clean:
	rm -rf $(BINARY_NAME) dist/

.PHONY: all build test clean
