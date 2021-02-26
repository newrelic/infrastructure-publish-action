SHELL := /bin/bash
WORKDIR := /home/gha

default: check-env prepare-schema dest-prefix mount-s3 mount-s3-check prepare-schema import-GPG-key publish-artifacts unmount-s3

check-env:
ifndef GPG_PRIVATE_KEY_BASE64
	$(error GPG_PRIVATE_KEY_BASE64 is undefined)
endif
ifndef GPG_KEY_RING
	$(error GPG_KEY_RING is undefined)
endif
ifndef GPG_PASSPHRASE
	$(error GPG_PASSPHRASE is undefined)
endif
ifndef AWS_SECRET_ACCESS_KEY
	$(error AWS_SECRET_ACCESS_KEY is undefined)
endif
ifndef AWS_ACCESS_KEY_ID
	$(error AWS_ACCESS_KEY_ID is undefined)
endif
ifndef AWS_S3_BUCKET_NAME
	$(error AWS_S3_BUCKET_NAME is undefined)
endif
ifndef AWS_ROLE_ARN
	$(error AWS_ROLE_ARN is undefined)
endif
ifndef ARTIFACTS_DEST_FOLDER
	$(error ARTIFACTS_DEST_FOLDER is undefined)
endif

prepare-schema: check-env
	@echo "prepare schema file for: $(SCHEMA)"
ifeq ($(SCHEMA), ohi)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/ohi.yml")
endif
ifeq ($(SCHEMA), nrjmx)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/nrjmx.yml")
endif
ifeq ($(SCHEMA), infra-agent)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/infra-agent.yml")
endif
ifeq ($(SCHEMA), custom)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/custom.yml")
	@wget "$(SCHEMA_URL)" -O  $(UPLOAD_SCHEMA_FILE_PATH)
	@echo "Print downloaded schema:"
	@cat ${UPLOAD_SCHEMA_FILE_PATH}
endif

dest-prefix: prepare-schema
	@echo "prepare destination prefix"
ifeq ($(ENV), pre-release)
	$(eval DEST_PREFIX := "infrastructure_agent/test/")
endif
ifeq ($(ENV), release)
	$(eval DEST_PREFIX := "infrastructure_agent/")
endif

mount-s3:
	@echo "Mounting S3 bucket $(AWS_S3_BUCKET_NAME) into $(AWS_S3_MOUNT_DIRECTORY)"
	@echo "Assuming IAM role for service account..."
	$(eval creds := $(shell AWS_PAGER="" aws sts assume-role \
			--output text \
			--role-arn $(AWS_ROLE_ARN) \
			--role-session-name $(AWS_ROLE_SESSION_NAME) \
		| tail -1 \
		| awk ' { print "AWS_ACCESS_KEY_ID=\"" $$2 "\" " \
						"AWS_SECRET_ACCESS_KEY=\"" $$4 "\" " \
						"AWS_SESSION_TOKEN=\"" $$5 "\" "}'))
	@echo "Mounting S3 into $(AWS_S3_MOUNT_DIRECTORY)..."
	$(shell $(creds) goofys $(AWS_S3_BUCKET_NAME) $(AWS_S3_MOUNT_DIRECTORY))

mount-s3-check: mount-s3
	@echo "Calculate size s3 bucket to confirm mount"
	@du -sh $(AWS_S3_MOUNT_DIRECTORY)

publish-artifacts: import-GPG-key mount-s3-check
	@echo "Publish artifacts"
	# AWS_ACCESS_KEY_ID is required for goofys.
	@UPLOAD_SCHEMA_FILE_PATH=$(UPLOAD_SCHEMA_FILE_PATH) DEST_PREFIX=$(DEST_PREFIX) AWS_ACCESS_KEY_ID=$(AWS_ACCESS_KEY_ID) /bin/publisher

unmount-s3: publish-artifacts
	@echo "Unmounting S3"
	@umount $(AWS_S3_MOUNT_DIRECTORY)

import-GPG-key:
	@printf %s $(GPG_PRIVATE_KEY_BASE64) | base64 --decode | gpg --batch --import --no-default-keyring --keyring $(GPG_KEY_RING) -

.PHONY: mount-s3 mount-s3-check publish-artifacts prepare-schema dest-prefix unmount-s3 import-GPG-key