export GO111MODULE = on
export GOPROXY = https://proxy.golang.org

ORGNAME := wabarc
PROJECT := screenshot
HOMEDIR ?= /go/src/github.com/${ORGNAME}
DOCKER ?= $(shell which docker || which podman)
IMAGE := wabarc/golang-chromium:dev

.PHONY: fmt

fmt:
	@echo "-> Running go fmt"
	@go fmt ./...

run:
	@echo "-> Running docker container"
	$(DOCKER) run -ti --rm -v ${PWD}/../:${HOMEDIR} ${IMAGE} sh -c "\
		cd ${HOMEDIR}/${PROJECT} && \
		go get -v && \
		sh"

test:
	@echo "-> Running go test"
	$(DOCKER) run -ti --rm -v ${PWD}/../:${HOMEDIR} ${IMAGE} sh -c "\
		cd ${HOMEDIR}/${PROJECT} && \
		go test -v ./..."

vet:
	@echo "-> Running go vet"
	@go vet ./...

clean:
	@echo "-> Cleaning"
	rm -rf *.png *.jpg *.jpe
