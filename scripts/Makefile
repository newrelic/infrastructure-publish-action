SHELL := /bin/bash
WORKDIR := /home/gha

default: check-env prepare-schema mount-s3 mount-s3-check prepare-schema import-GPG-key publish-artifacts unmount-s3 clear-fastly-cache

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
	@echo "Downloaded schema:"
	@cat ${UPLOAD_SCHEMA_FILE_PATH}
endif

mount-s3:
	@echo "Assuming IAM role for service account..."
	./mount-s3.sh

mount-s3-check: mount-s3
	@echo "Confirm s3 mount..."
	@mount |grep $(AWS_S3_MOUNT_DIRECTORY)

publish-artifacts: import-GPG-key mount-s3-check
	@echo "Publish artifacts"
	@UPLOAD_SCHEMA_FILE_PATH=$(UPLOAD_SCHEMA_FILE_PATH) DEST_PREFIX="infrastructure_agent/" /bin/publisher

unmount-s3: publish-artifacts
	@echo "Unmounting S3..."
	@umount $(AWS_S3_MOUNT_DIRECTORY)

import-GPG-key:
	@printf %s $(GPG_PRIVATE_KEY_BASE64) | base64 --decode | gpg --batch --import --no-default-keyring --keyring $(GPG_KEY_RING) -

clear-fastly-cache: unmount-s3
ifdef FASTLY_KEY
	@/usr/bin/curl -i -X POST -H Fastly-Key:$(FASTLY_KEY) https://api.fastly.com/service/$(FASTLY_SERVICE_ID)/purge_all
endif

.PHONY: mount-s3 mount-s3-check publish-artifacts prepare-schema unmount-s3 import-GPG-key clear-fastly-cache