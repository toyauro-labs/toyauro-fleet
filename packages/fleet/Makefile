.PHONY: build test dist

build:
	go build -o bin/fleet .

test: build
	go vet ./...
	go test ./...
	FLEET_BIN=$(CURDIR)/bin/fleet python3 -m unittest discover -s tests

# No cgo, so each binary runs on its device without a toolchain there.
dist:
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/fleet-darwin-arm64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/fleet-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o dist/fleet-linux-arm64 .
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -o dist/fleet-android-arm64 .
