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
	@rm -fr .coverdata
	@mkdir -p .coverdata/unit .coverdata/integration
	@gotestsum -- -cover ./internal/... -test.gocoverdir="$$(pwd)/.coverdata/unit"
	@gotestsum ./integration/... $(UPDATE_FLAG) $(UPDATE_ALL_FLAG)
	@go tool covdata percent -i=.coverdata/integration,.coverdata/unit
	@go tool covdata textfmt -i=.coverdata/integration,.coverdata/unit -o .coverdata/coverage.out

check-coverage: test
	@go tool covdata textfmt -i=.coverdata/integration,.coverdata/unit -o .coverdata/profile.txt
	@go tool cover -html=profile.txt

build:
	@go build -o dist/grog

build-with-coverage:
	@go build -cover -o dist/grog

run:
	go run main.go $(MAKECMDGOALS)

run-repo: build
	@cd integration/test_repos/$(path) && ../../dist/grog $(MAKECMDGOALS)


.DEFAULT_GOAL := build
