SHELL := /bin/bash

default: check-env prepare-secrets mount-s3 mount-s3-check publish-artifacts unmount-s3

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

prepare-secrets: check-env
	@echo "Generating secrets file into /etc/passwd-s3fs"
	@echo $(AWS_ACCESS_KEY):$(AWS_SECRET_ACCESS_KEY) > /etc/passwd-s3fs
	@chmod 600 /etc/passwd-s3fs

mount-s3: prepare-secrets
	@echo "Mounting S3 into $(AWS_S3_MOUNT_DIRECTORY)"
	@s3fs $(AWS_S3_BUCKET_NAME) $(ARTIFACTS_DEST_FOLDER)

mount-s3-check: mount-s3
	@echo "List files from s3 bucket to confirm mount"
	@du -sh $(AWS_S3_MOUNT_DIRECTORY)

publish-artifacts: mount-s3-check
	@echo "Publish artifacts"
	@env
	@/bin/publisher

unmount-s3: publish-artifacts
	@echo "Unmounting S3"
	@umount $(ARTIFACTS_DEST_FOLDER)

.PHONY: prepare-secrets mount-s3 mount-s3-check publish-artifacts unmount-s3