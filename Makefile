export GO111MODULE = on
export CGO_ENABLED = 0
export GOPROXY = https://proxy.golang.org

ORGNAME := wabarc
PROJECT := screenshot
BINDIR ?= ./build/binary
PACKDIR ?= ./build/package
LDFLAGS := $(shell echo "-X 'telegra.ph/version.Version=`git describe --tags --abbrev=0`'")
LDFLAGS := $(shell echo "${LDFLAGS} -X 'telegra.ph/version.Commit=`git rev-parse --short HEAD`'")
LDFLAGS := $(shell echo "${LDFLAGS} -X 'telegra.ph/version.BuildDate=`date +%FT%T%z`'")
GOBUILD ?= go build -trimpath --ldflags "-s -w ${LDFLAGS} -buildid=" -v
GOFILES ?= $(wildcard ./cmd/${PROJECT}/*.go)
VERSION ?= $(shell git describe --tags `git rev-list --tags --max-count=1` | sed -e 's/v//g')
PACKAGES ?= $(shell go list ./...)
HOMEDIR ?= /go/src/github.com/${ORGNAME}
DOCKER ?= $(shell which docker || which podman)
IMAGE := wabarc/golang-chromium:dev
NAME := ${PROJECT}

PLATFORM_LIST = \
	darwin-amd64 \
	darwin-arm64 \
	linux-386 \
	linux-amd64 \
	linux-arm64

WINDOWS_ARCH_LIST = \
	windows-386 \
	windows-amd64

.PHONY: \
	darwin-386 \
	darwin-amd64 \
	linux-386 \
	linux-amd64 \
	linux-arm64 \
	windows-386 \
	windows-amd64 \
	all-arch \
	tar_releases \
	zip_releases \
	releases \
	clean \
	test \
	fmt \
	vet

darwin-amd64:
	GOARCH=amd64 GOOS=darwin $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ $(GOFILES)

darwin-arm64:
	GOARCH=arm64 GOOS=darwin $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ $(GOFILES)

linux-386:
	GOARCH=386 GOOS=linux $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ $(GOFILES)

linux-amd64:
	GOARCH=amd64 GOOS=linux $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ $(GOFILES)

linux-arm64:
	GOARCH=arm64 GOOS=linux $(GOBUILD) -o $(BINDIR)/$(NAME)-$@ $(GOFILES)

windows-386:
	GOARCH=386 GOOS=windows $(GOBUILD) -o $(BINDIR)/$(NAME)-$@.exe $(GOFILES)

windows-amd64:
	GOARCH=amd64 GOOS=windows $(GOBUILD) -o $(BINDIR)/$(NAME)-$@.exe $(GOFILES)

ifeq ($(TARGET),)
tar_releases := $(addsuffix .gz, $(PLATFORM_LIST))
zip_releases := $(addsuffix .zip, $(WINDOWS_ARCH_LIST))
else
ifeq ($(findstring windows,$(TARGET)),windows)
zip_releases := $(addsuffix .zip, $(TARGET))
else
tar_releases := $(addsuffix .gz, $(TARGET))
endif
endif

$(tar_releases): %.gz : %
	chmod +x $(BINDIR)/$(NAME)-$(basename $@)
	tar -czf $(PACKDIR)/$(NAME)-$(basename $@)-$(VERSION).tar.gz --transform "s/.*\///g" $(BINDIR)/$(NAME)-$(basename $@)

$(zip_releases): %.zip : %
	zip -m -j $(PACKDIR)/$(NAME)-$(basename $@)-$(VERSION).zip $(BINDIR)/$(NAME)-$(basename $@).exe

all-arch: $(PLATFORM_LIST) $(WINDOWS_ARCH_LIST)

releases: $(tar_releases) $(zip_releases)

fmt:
	@echo "-> Running go fmt"
	@go fmt ./...

run:
	@echo "-> Running docker container"
	$(DOCKER) run --memory="500m" -ti --rm -v ${PWD}/../:${HOMEDIR} ${IMAGE} sh -c "\
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
	@echo "-> Cleaning..."
	@rm -rf *.png *.jpg *.jpe* *.pdf *.htm* *.har
	@rm -rf build/binary/* build/package/*
