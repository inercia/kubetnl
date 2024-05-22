PLATFORMS   := linux/amd64 windows/amd64 darwin/amd64

VERSION      = $(shell git describe HEAD --tags --abbrev=0 2>/dev/null)
GIT_COMMIT   = $(shell git rev-parse HEAD 2>/dev/null)
LD_FLAGS     = -ldflags="-X 'github.com/inercia/kubetnl/pkg/version.gitCommit=$(GIT_COMMIT)'"

MAIN         = ./main.go
SRCS         = $(shell find . -name '*.go' ! -path './tests/*' 2>/dev/null)
ALL_SRCS     = $(shell find . -name '*.go' 2>/dev/null)
SHS          = $(shell find . -name '*.sh' 2>/dev/null)

GO          ?= go
GOOS        ?= $(shell go env GOOS 2>/dev/null)
GOARCH      ?= $(shell go env GOARCH 2>/dev/null)
GOEXE       ?= kubetnl
GOFLAGS     ?=

SHFMT_ARGS   = -s -ln bash

############################################################################################

.DEFAULT_GOAL:=help

.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

#########################################
##@ Build
#########################################

bin/kubetnl-$(VERSION)_windows-amd64/kubetnl.exe: GOARCH=amd64
bin/kubetnl-$(VERSION)_windows-amd64/kubetnl.exe: GOOS=windows
bin/kubetnl-$(VERSION)_windows-amd64/kubetnl.exe: GOEXE=kubetnl.exe

bin/kubetnl-$(VERSION)_linux-amd64/kubetnl: GOARCH=amd64
bin/kubetnl-$(VERSION)_linux-amd64/kubetnl: GOOS=linux

bin/kubetnl-$(VERSION)_darwin-amd64/kubetnl: GOARCH=amd64
bin/kubetnl-$(VERSION)_darwin-amd64/kubetnl: GOOS=darwin

bin/kubetnl-$(VERSION)_darwin-arm64/kubetnl: GOARCH=arm64
bin/kubetnl-$(VERSION)_darwin-arm64/kubetnl: GOOS=darwin

bin/%: $(SRCS)
	@echo ">>> Building $(GOEXE) executable (OS='$(GOOS)', ARCH='$(GOARCH)')"
	go build ${LD_FLAGS} -o 'bin/kubetnl-$(VERSION)_$(GOOS)-$(GOARCH)/$(GOEXE)' ${MAIN}

exe: bin/kubetnl-$(VERSION)_$(GOOS)-$(GOARCH)/$(GOEXE) ## Build the default executable
build: exe

windows-amd64: bin/kubetnl-$(VERSION)_windows-amd64/kubetnl.exe
linux-amd64:   bin/kubetnl-$(VERSION)_linux-amd64/kubetnl
darwin-amd64:  bin/kubetnl-$(VERSION)_darwin-amd64/kubetnl
darwin-arm64:  bin/kubetnl-$(VERSION)_darwin-arm64/kubetnl

clean: ## Clean all the artifacts
	rm -rf bin

version:
	echo ${VERSION}

#########################################
##@ Release
#########################################

release: clean windows-amd64 linux-amd64 darwin-amd64 darwin-arm64    ## Create release executables
	zip -r bin/kubetnl-$(VERSION)_windows-amd64.zip bin/kubetnl-$(VERSION)_windows-amd64
	zip -r bin/kubetnl-$(VERSION)_linux-amd64.zip bin/kubetnl-$(VERSION)_linux-amd64
	zip -r bin/kubetnl-$(VERSION)_darwin-amd64.zip bin/kubetnl-$(VERSION)_darwin-amd64
	zip -r bin/kubetnl-$(VERSION)_darwin-arm64.zip bin/kubetnl-$(VERSION)_darwin-arm64

#########################################
##@ Code fomatting
#########################################

format-go: ## Format the Go source code
	@echo ">>> Formatting the Go source code..."
	GOFLAGS="$(GOFLAGS)" $(GO) fmt `$(GO) list ./...`

format-sh:  ## Format the Shell source code
	@echo ">>> Formatting the Shell source code..."
	echo "$(SHS)" | xargs shfmt $(SHFMT_ARGS) -w 

format-all: format-go format-sh
format: format-all ## Format all the code
fmt: format

#########################################
##@ Tests
#########################################

PHONY: tests
tests: run-tests
run-tests: $(ALL_SRCS) ## Run all the tests
	go test ./tests -test.v
