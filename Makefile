VERSION := $(shell  git log -1 --format="%ad+%h" --date=format:"%Y.%j.%H%M")

.PHONY: all
all: server project.yaml

project.yaml: project.yaml.in
	@sed -e 's|__VERSION__|$(VERSION)|g' -e 's|\.0*|.|g' project.yaml.in > $@

.PHONY: dir
dir:
	@mkdir -p build/resources

.PHONY: server
server: dir
	@mkdir -p build/resources && \
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -o build/resources/webdav-server-amd64 -trimpath -ldflags '-w -s ' . && \
	env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -v -o build/resources/webdav-server-arm64 -trimpath -ldflags '-w -s ' .


.PHONY: fmt
fmt:
	@(test -f "$(GOPATH)/bin/gofumpt" || go install mvdan.cc/gofumpt@latest) && \
	"$(GOPATH)/bin/gofumpt" -l -w .