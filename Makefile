VERSION=0.4.0
LDFLAGS=-ldflags "-w -s -X main.Version=${VERSION}"
all: wsgate-server

.PHONY: wsgate-server

wsgate-server: wsgate-server.go
	go build $(LDFLAGS) -o wsgate-server

linux: wsgate-server.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o wsgate-server

fmt:
	go fmt ./...

clean:
	rm -rf wsgate-server

