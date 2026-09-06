BINARY_NAME=zoraxy-geoheaders

all: build

build:
	go build -ldflags="-s -w" -o $(BINARY_NAME) .

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)

.PHONY: all build test clean
