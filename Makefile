SOURCE_DIR = $(CURDIR)/publisher
TEST_E2E_DIR = $(CURDIR)/test
GO_FMT 	?= gofmt -s -w -l $(SOURCE_DIR)

.PHONY: deps
deps:
	@printf '\n------------------------------------------------------\n'
	@printf 'Installing package dependencies required by the project.\n'
	(cd $(SOURCE_DIR) && go mod vendor)
	@echo 'Success.'

.PHONY: validate
validate:
	@printf '\n------------------------------------------------------\n'
	@printf 'Validating source code running golangci-lint.\n'
	@test -z "$(SHELL  $(GO_FMT) | tee /dev/stderr)"
	@echo 'Success.'

.PHONY: test
test: deps
	@printf '\n------------------------------------------------------\n'
	@printf 'Running unit tests.\n'
	(cd $(SOURCE_DIR) && go test -race ./...)
	@echo 'Success.'

.PHONY: test-e2e
test-e2e: deps
	@printf '\n------------------------------------------------------\n'
	@printf 'Running system tests: publish-agent.\n'
	ROOT_DIR=$(CURDIR) $(TEST_E2E_DIR)/publish-agent.sh
	@echo 'Success.'
