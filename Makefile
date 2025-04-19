VERSION=0.4.1
LDFLAGS=-ldflags "-w -s -X main.Version=${VERSION}"
all: wsgate-server

.PHONY: wsgate-server

wsgate-server: cmd/wsgate-server/main.go
	go build $(LDFLAGS) -o wsgate-server cmd/wsgate-server/main.go

linux: cmd/wsgate-server/main.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o wsgate-server cmd/wsgate-server/main.go

check:
	go test -v ./...

fmt:
	go fmt ./...

clean:
	rm -rf wsgate-server

