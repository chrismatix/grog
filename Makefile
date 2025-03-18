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

test: build-with-coverage
	@rm -fr .coverdata
	@mkdir -p .coverdata
	@gotestsum ./internal/...
	@gotestsum ./integration/... $(UPDATE_FLAG) $(UPDATE_ALL_FLAG)
	@go tool covdata percent -i=.coverdata

check-coverage: test
	@go tool covdata textfmt -i=.coverdata -o profile.txt
	@go tool cover -html=profile.txt

build:
	@go build -o dist/grog

build-with-coverage:
	@go build -cover -o dist/grog

run:
	go run main.go

.DEFAULT_GOAL := build
