
## TEST
ifeq ($(update-all),true)
UPDATE_FLAG := -update-all
else
UPDATE_FLAG :=
endif

ifneq ($(update),)
UPDATE_ALL_FLAG := -update=$(update)
else
UPDATE_ALL_FLAG :=
endif

unit-test:
	@gotestsum ./internal/...

# TODO combine all this cover data and show on github usin
# https://github.com/tj-actions/coverage-badge-go
# https://dustinspecker.com/posts/go-combined-unit-integration-code-coverage/
test: build-with-coverage
	@rm -fr coverdata
	@mkdir -p coverdata/unit coverdata/integration
	@gotestsum -- -timeout 60s -cover ./internal/... -test.gocoverdir="$$(pwd)/coverdata/unit"
	@gotestsum -- -timeout 60s ./integration/... $(UPDATE_FLAG) $(UPDATE_ALL_FLAG)
	@go tool covdata percent -i=coverdata/integration,coverdata/unit
	@go tool covdata textfmt -i=coverdata/integration,coverdata/unit -o coverdata/coverage.out

check-coverage: test
	@go tool covdata textfmt -i=coverdata/integration,coverdata/unit -o coverdata/profile.txt
	@go tool cover -html=profile.txt


## BUILD
# Version and build metadata
VERSION   ?= $(shell git describe --tags --always 2>/dev/null)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE      ?= $(shell date +%FT%T%z)

LD_FLAGS = -ldflags "\
	-X 'main.version=$(VERSION)' \
	-X 'main.commit=$(COMMIT)' \
	-X 'main.buildDate=$(DATE)'"

build:
	@go build -o dist/grog $(LD_FLAGS)

build-with-coverage:
	@go build -cover -o dist/grog $(LD_FLAGS)

release-pkl:
	@cd pkl && VERSION=$(VERSION) pkl project package


# Build release targets for static binaries across architectures.
release: clean
	@echo "Building release binaries"
	@mkdir -p dist

	$(MAKE) release-build GOOS=linux GOARCH=amd64
	$(MAKE) release-build GOOS=linux GOARCH=arm64
	$(MAKE) release-build GOOS=darwin GOARCH=amd64
	$(MAKE) release-build GOOS=darwin GOARCH=arm64
	$(MAKE) release-build GOOS=windows GOARCH=amd64

	$(MAKE) release-pkl

	@shasum -a 256 dist/* > dist/SHASUMS256.txt

	@echo "Release build completed."

# CGO_ENABLED=0 ensures static linking.
release-build:
	@echo "Building for $(GOOS)/$(GOARCH)"
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LD_FLAGS) -o dist/grog-$(GOOS)-$(GOARCH)$(if $(findstring windows,$(GOOS)),.exe,)

# Clean built binaries
clean:
	@rm -rf dist

## RUN
run:
	@go run main.go $(MAKECMDGOALS)

run-repo: build
	@cd integration/test_repos/$(path) && ../../dist/grog $(MAKECMDGOALS)

gen-docs: # Re-generate cli docs
	cd docs && go run gen_docs.go

.DEFAULT_GOAL := build
