SHELL := /bin/bash
WORKDIR := /home/gha

default: check-env mount-s3 mount-s3-check prepare-schema import-GPG-key publish-artifacts unmount-s3

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
ifndef AWS_REGION
	$(error AWS_REGION is undefined)
endif
ifndef AWS_ROLE_ARN
	$(error AWS_ROLE_ARN is undefined)
endif
ifndef ARTIFACTS_DEST_FOLDER
	$(error ARTIFACTS_DEST_FOLDER is undefined)
endif

prepare-schema: check-env
	@echo "Prepare schema file for: $(SCHEMA)"
ifeq ($(SCHEMA), custom)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/custom.yml")
	@wget "$(SCHEMA_URL)" -O  $(UPLOAD_SCHEMA_FILE_PATH)
	@echo "Downloaded schema:"
	@cat ${UPLOAD_SCHEMA_FILE_PATH}
else
ifeq ($(SCHEMA), custom-local)
	@if [[ ! -f "$(SCHEMA_PATH)" ]]; then \
		echo "Cannot find \"$(SCHEMA_PATH)\""; \
		exit 1; \
	fi
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(SCHEMA_PATH)")
else
	@if [[ ! -f "$(WORKDIR)/schemas/$(SCHEMA).yml" ]]; then \
		echo "Cannot find $(SCHEMA).yml file in $(WORKDIR)/schemas/"; \
		exit 1; \
	fi
	$(eval UPLOAD_SCHEMA_FILE_PATH := "$(WORKDIR)/schemas/$(SCHEMA).yml")
endif
endif

mount-s3:
	@echo "Assuming IAM role for service account..."
	./mount-s3.sh

mount-s3-check: mount-s3
	@echo "Confirm s3 mount..."
	@mount |grep $(AWS_S3_MOUNT_DIRECTORY)

DEST_PREFIX ?= "infrastructure_agent/"
publish-artifacts: mount-s3-check
	@echo "Publish artifacts"
	@UPLOAD_SCHEMA_FILE_PATH=$(UPLOAD_SCHEMA_FILE_PATH) DEST_PREFIX=$(DEST_PREFIX) /bin/publisher

unmount-s3:
	@echo "Unmounting S3..."
	@umount $(AWS_S3_MOUNT_DIRECTORY)

.PHONY: mount-s3 mount-s3-check publish-artifacts prepare-schema unmount-s3 import-GPG-key
