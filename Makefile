BIN=bin/eufy-wall
PKG=./cmd/eufy-wall
LDFLAGS=-s -w

.PHONY: test host pi1 pi3 pi64 amd64 all clean
test:
	go test ./...
host:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)
pi1:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "$(LDFLAGS)" -o $(BIN)-armv6 $(PKG)
pi3:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o $(BIN)-armv7 $(PKG)
pi64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)-arm64 $(PKG)
amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)-amd64 $(PKG)
all: pi1 pi3 pi64 amd64
clean:
	rm -rf bin
