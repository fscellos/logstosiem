
BINARY:=labgo
TAG:=v1.0
IMAGE:=labgo:${TAG}
TOUT=test build docker-build
IN = $(shell find . -type f -name '*.go')
OUT = $(IN:.go=.bin)


printallfiles: ${OUT}

%.bin : %.go
	@echo "Nouveau fichier " $*.bin

.PHONY: build
build:
	@echo "on lance le build"
	go build -x -o bin/${BINARY}

.PHONY: docker-build
docker-build:
	docker build -t ${IMAGE} .

.PHONY: test
test:
	@echo ${fichier}
	go test ./...

.PHONY: list
	list: main.go

.PHONY: all
all: ${TOUT}