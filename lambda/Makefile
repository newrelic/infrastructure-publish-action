TERRAFORM_DIR := ./terraform

lambda: lambda.go
	GOARCH=amd64 GOOS=linux go build

lambda.zip: lambda
	zip $@ lambda

.PHONY: build
build: lambda lambda.zip

.PHONY: init
init: clean lambda.zip
	terraform -chdir=$(TERRAFORM_DIR) init

.PHONY: plan
plan: init
	terraform -chdir=$(TERRAFORM_DIR) plan

.PHONY: apply
apply: init
	terraform -chdir=$(TERRAFORM_DIR) apply

.PHONY: clean
clean:
	rm -f lambda.zip lambda
