GO_FMT 	?= gofmt -s -w -l $(CURDIR)/publisher

.PHONY: deps
deps:
	@printf '\n------------------------------------------------------\n'
	@printf 'Installing package dependencies required by the project.\n'
	go mod download
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
	go test -race ./...
	@echo 'Success.'
