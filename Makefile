GO111MODULE=off
GOARCH=amd64
GOOS=darwin
# GOOS=linux
GO_PACKAGE=github.com/alibaba/open-simulator
CGO_ENABLED=0

COMMITID=$(shell git rev-parse --short HEAD)
VERSION=v0.1.1-dev
LD_FLAGS=-ldflags "-X '${GO_PACKAGE}/cmd/version.VERSION=$(VERSION)' -X '${GO_PACKAGE}/cmd/version.COMMITID=$(COMMITID)'"

OUTPUT_DIR=./bin
BINARY_NAME=simon
LINUX_BINARY_NAME=simon_linux

all: build run

.PHONY: build 
build:
	GO111MODULE=$(GO111MODULE) GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 go build -trimpath $(LD_FLAGS) -v -o $(OUTPUT_DIR)/$(BINARY_NAME) ./cmd
	# chmod +x $(OUTPUT_DIR)/$(BINARY_NAME)
	# bin/simon apply -i -f ./example/simon-config.yaml

.PHONY: run
run:
	# bin/simon apply --extended-resources "gpu" -f example/simon-gpushare-config.yaml
	$(OUTPUT_DIR)/simon apply --extended-resources "gpu" -f example/simon-paib-snapshot-add-config.yaml

.PHONY: linux
linux:
	GO111MODULE=$(GO111MODULE) GOARCH=$(GOARCH) GOOS=linux CGO_ENABLED=0 go build -trimpath $(LD_FLAGS) -v -o $(OUTPUT_DIR)/$(LINUX_BINARY_NAME) ./cmd
	chmod +x $(OUTPUT_DIR)/simon_linux

.PHONY: linuxexp
exp:
	$(OUTPUT_DIR)/$(LINUX_BINARY_NAME) apply --extended-resources "gpu" -f example/paib/2022_03_13_11_30_06/paib_config_2022_03_13_11_30_06_100.yaml  --default-scheduler-config example/scheduler-config/scheduler-config.yaml

.PHONY: test 
test:
	go test -v ./...

.PHONY: clean 
clean:
	rm -rf ./bin || true
