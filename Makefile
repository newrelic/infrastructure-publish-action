SOURCE_DIR = $(CURDIR)/publisher
TEST_E2E_DIR = $(CURDIR)/test
GO_FMT 	?= gofmt -s -w -l $(SOURCE_DIR)

ifndef TAG
TAG := dev
endif

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
test-e2e: deps docker/build
	@printf '\n------------------------------------------------------\n'
	@printf 'Running system tests: publish-agent.\n'
	ROOT_DIR=$(CURDIR) $(TEST_E2E_DIR)/publish-agent.sh
	@echo 'Success.'

.PHONY: docker/build
docker/build:
	@printf '\n------------------------------------------------------\n'
	@printf 'Building docker image\n'
	docker build \
		 -t ohaiops/infrastructure-publish-action:$(TAG)\
		 -t ohaiops/infrastructure-publish-action:latest\
		 -f ./Dockerfile .

.PHONY: docker/publish
docker/publish:
	@printf '\n------------------------------------------------------\n'
	@printf 'Publishing docker image\n'
	docker push ohaiops/infrastructure-publish-action:$(TAG)
	docker push ohaiops/infrastructure-publish-action:latest