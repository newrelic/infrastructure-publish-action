SHELL := /bin/bash

default: check-env prepare-schema dest-prefix prepare-secrets mount-s3 mount-s3-check prepare-schema import-GPG-key publish-artifacts unmount-s3

check-env:
ifndef AWS_SECRET_ACCESS_KEY
	$(error AWS_SECRET_ACCESS_KEY is undefined)
endif
ifndef AWS_ACCESS_KEY
	$(error AWS_ACCESS_KEY is undefined)
endif
ifndef AWS_S3_BUCKET_NAME
	$(error AWS_S3_BUCKET_NAME is undefined)
endif
ifndef GPG_PRIVATE_KEY_BASE64
	$(error GPG_PRIVATE_KEY_BASE64 is undefined)
endif
ifndef GPG_KEY_RING
	$(error GPG_KEY_RING is undefined)
endif
ifndef GPG_PASSPHRASE
	$(error GPG_PASSPHRASE is undefined)
endif


prepare-schema: check-env
	@echo "prepare schema file for: ${SCHEMA}"
ifeq ($(SCHEMA), ohi)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "/home/gha/schemas/ohi.yml")
endif
ifeq ($(SCHEMA), nrjmx)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "/home/gha/schemas/nrjmx.yml")
endif
ifeq ($(SCHEMA), infra-agent)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "/home/gha/schemas/infra-agent.yml")
endif
ifeq ($(SCHEMA), custom)
	$(eval UPLOAD_SCHEMA_FILE_PATH := "/home/gha/schemas/custom.yml")
	@wget "${SCHEMA_URL}" -O  ${UPLOAD_SCHEMA_FILE_PATH}
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

prepare-secrets: dest-prefix
	@echo "Generating secrets file into /etc/passwd-s3fs"
	@echo $(AWS_ACCESS_KEY):$(AWS_SECRET_ACCESS_KEY) > /etc/passwd-s3fs
	@chmod 600 /etc/passwd-s3fs

mount-s3: prepare-secrets
	@echo "Mounting S3 into $(AWS_S3_MOUNT_DIRECTORY)"
	@s3fs $(AWS_S3_BUCKET_NAME) $(ARTIFACTS_DEST_FOLDER)

mount-s3-check: mount-s3
	@echo "Calculate size s3 bucket to confirm mount"
	@du -sh $(AWS_S3_MOUNT_DIRECTORY)

publish-artifacts: import-GPG-key mount-s3-check
	@echo "Publish artifacts"
	@UPLOAD_SCHEMA_FILE_PATH=${UPLOAD_SCHEMA_FILE_PATH} DEST_PREFIX=${DEST_PREFIX} /bin/publisher

unmount-s3: publish-artifacts
	@echo "Unmounting S3"
	@umount $(ARTIFACTS_DEST_FOLDER)

import-GPG-key:
	@printf %s ${GPG_PRIVATE_KEY_BASE64} | base64 --decode | gpg --batch --import --no-default-keyring --keyring ${GPG_KEY_RING} -

.PHONY: prepare-secrets mount-s3 mount-s3-check publish-artifacts prepare-schema dest-prefix unmount-s3 import-GPG-key