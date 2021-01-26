export GO111MODULE = on
export GOPROXY = https://proxy.golang.org

ORGNAME := wabarc
PROJECT := screenshot
SRCPATH ?= /go/src/github.com/${ORGNAME}/${PROJECT}
DOCKER ?= $(shell which docker || which podman)
IMAGE := wabarc/golang-chromium:dev

.PHONY: fmt

fmt:
	@echo "-> Running go fmt"
	@go fmt ./...

run:
	@echo "-> Running docker container"
	$(DOCKER) run -ti --rm -v ${PWD}:${SRCPATH} ${IMAGE} sh -c "\
		cd ${SRCPATH} && \
		go get -v && \
		sh"

test:
	@echo "-> Running go test"
	$(DOCKER) run -ti --rm -v ${PWD}:${SRCPATH} ${IMAGE} sh -c "\
		cd ${SRCPATH} && \
		go test -v ./..."

vet:
	@echo "-> Running go vet"
	@go vet ./...

clean:
	@echo "-> Cleaning"
	rm -rf *.png *.jpg *.jpe
