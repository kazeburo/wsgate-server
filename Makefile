VERSION=0.0.4
LDFLAGS=-ldflags "-X main.Version=${VERSION}"
all: wsgate-server

.PHONY: wsgate-server

bundle:
	dep ensure

update:
	dep ensure -update

wsgate-server: wsgate-server.go
	go build $(LDFLAGS) -o wsgate-server

linux: wsgate-server.go
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o wsgate-server

fmt:
	go fmt ./...

clean:
	rm -rf wsgate-server

tag:
	git tag v${VERSION}
	git push origin v${VERSION}
	git push origin master
	goreleaser --rm-dist
