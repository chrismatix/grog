ifeq ($(update),true)
UPDATE_FLAG := -update
else
UPDATE_FLAG :=
endif


test: build-with-coverage
	@rm -fr .coverdata
	@mkdir -p .coverdata
	@gotestsum ./... $(UPDATE_FLAG)
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
