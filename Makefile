# Use bash so `set -o pipefail` works (needed to capture pytest exit codes
# through a `tee` pipe in the test-all summary).
SHELL    := bash

PYTHON   ?= python3
VENV     := .venv
PIP      := $(VENV)/bin/pip
PYTEST   := $(VENV)/bin/pytest
GO_TEST_IMAGE := appie-backend-test

# Where to run the Go checks (gofmt/vet/test): `local` uses the Go toolchain on
# PATH (fast, no image build), `docker` runs them inside the backend test image.
# test.sh sets this automatically; override manually with e.g.
# `make go-test GO_RUNTIME=docker`.
GO_RUNTIME ?= local

# Shared pytest selections so test-all stays in sync with the individual targets.
UNIT_ARGS        := -m "not integration" tests/
INTEGRATION_ARGS := -m integration -s tests/integration/

.PHONY: venv test-fast test-unit test-integration test-all clean-venv go-test go-lint go-test-image test-login

## Create the test virtual environment and install all dependencies.
venv: $(VENV)/bin/pytest

$(VENV)/bin/pytest: tests/requirements.txt
	$(PYTHON) -m venv $(VENV)
	$(PIP) install --quiet --upgrade pip
	$(PIP) install --quiet -r tests/requirements.txt

## Build the backend test image used by the Go lint and test targets.
go-test-image:
	docker build --target test -t $(GO_TEST_IMAGE) backend

## Check Go formatting and run go vet. Fails if any file is not gofmt-clean.
go-lint:
ifeq ($(GO_RUNTIME),docker)
	$(MAKE) --no-print-directory go-test-image
	docker run --rm $(GO_TEST_IMAGE) sh -c '\
			unformatted=$$(gofmt -l .); \
			if [ -n "$$unformatted" ]; then \
				echo "gofmt needs to be run on:"; echo "$$unformatted"; exit 1; \
			fi; \
			go vet ./...'
else
	@unformatted=$$(cd backend && gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to be run on:"; echo "$$unformatted"; exit 1; \
	fi
	cd backend && go vet ./...
endif

## Run Go tests for the backend (includes syncer and analytics sub-packages).
go-test: go-lint
ifeq ($(GO_RUNTIME),docker)
	docker run --rm $(GO_TEST_IMAGE) sh -c 'go test ./...'
else
	cd backend && go test ./...
endif

## Run fast unit tests only (no model download required).
test-fast: venv go-test
	$(PYTEST) -m "not slow and not integration" tests/

## Run all unit tests.
test-unit: venv go-test
	$(PYTEST) $(UNIT_ARGS)

## Log in to the AH API and save credentials to ~/.config/appie/appie.json.
## Run this once before `make test-integration` when you don't have the Docker
## named volume (e.g. running the tests outside of Docker). Opens your browser;
## the login URL is also printed in case it needs to be opened manually.
test-login:
	cd backend && go run . -login

## Run integration tests (requires AH credentials via AH_ACCESS_TOKEN or config/appie.json).
## Fetches up to 3 receipts/orders by default; override with SYNC_MAX_RECEIPTS=N SYNC_MAX_ORDERS=N.
## Optionally keep the DB: make test-integration KEEP_DB=/tmp/ah_test.db
test-integration: venv go-test
	$(PYTEST) $(INTEGRATION_ARGS) $(if $(KEEP_DB),--keep-integration-db=$(KEEP_DB),)

## Run everything: Go lint+tests, unit tests, and integration tests. All three
## phases run independently (a failure in one does not skip the others) and a
## combined PASS/FAIL summary is printed at the bottom so no result is buried.
test-all: venv
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	grn=$$'\033[32m'; red=$$'\033[31m'; rst=$$'\033[0m'; \
	stat() { [ "$$1" -eq 0 ] && printf '%s' "$$grn""PASS""$$rst" || printf '%s' "$$red""FAIL""$$rst"; }; \
	pysum() { tail -n 1 "$$1" | tr -d '\r' | sed -E 's/\x1b\[[0-9;]*m//g' | tr -d '=' | sed 's/^ *//;s/ *$$//'; }; \
	run() { script -qec "$$1" /dev/null; }; \
	set -o pipefail; \
	echo "================= GO LINT + TESTS ================="; \
	run '$(MAKE) --no-print-directory go-test' | tee "$$tmp/go.log"; go=$$?; \
	echo "==================== UNIT TESTS ===================="; \
	run '$(PYTEST) $(UNIT_ARGS)' | tee "$$tmp/unit.log"; unit=$$?; \
	echo "================ INTEGRATION TESTS ================="; \
	run '$(PYTEST) $(INTEGRATION_ARGS)' | tee "$$tmp/integration.log"; integ=$$?; \
	echo; \
	echo "===================== SUMMARY ====================="; \
	printf "Go lint+tests: %s\n" "$$(stat $$go)"; \
	printf "Unit:          %s (%s)\n" "$$(stat $$unit)" "$$(pysum "$$tmp/unit.log")"; \
	printf "Integration:   %s (%s)\n" "$$(stat $$integ)" "$$(pysum "$$tmp/integration.log")"; \
	[ $$go -eq 0 ] && [ $$unit -eq 0 ] && [ $$integ -eq 0 ]


## Delete the test virtual environment.
clean-venv:
	rm -rf $(VENV)
