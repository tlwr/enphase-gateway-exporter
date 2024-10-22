lint:
	golangci-lint run ./...

build:
	GOOS=linux GOARCH=arm64 go build -o enphase-gateway-exporter
