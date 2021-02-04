ARG VERSION=1.15.7-buster
FROM golang:$VERSION

ARG AWS_S3_MOUNT_DIRECTORY=/mnt/s3
ENV AWS_S3_MOUNT_DIRECTORY $AWS_S3_MOUNT_DIRECTORY

# validate s3fs-fuse with the sec team
RUN apt-get update && \
  apt-get install -y \
            s3fs \
            make \
            curl \
            gnupg2 \
            bzip2 \
            createrepo

# Sadly installing aptly with apt lead you to a old version not supporting gpg2
RUN wget https://github.com/aptly-dev/aptly/releases/download/v1.4.0/aptly_1.4.0_linux_amd64.tar.gz
RUN tar xzf aptly_1.4.0_linux_amd64.tar.gz
RUN mv ./aptly_1.4.0_linux_amd64/aptly /usr/bin/aptly

RUN mkdir $AWS_S3_MOUNT_DIRECTORY

WORKDIR /home/gha

ADD publisher ./publisher
ADD schemas ./schemas
ADD Makefile .

RUN mkdir ./assets

WORKDIR ./publisher
#RUN go get -d -v
RUN go build -o /bin/publisher publisher.go

WORKDIR /home/gha
RUN chmod +x /bin/publisher

CMD ["make", "--jobs=1"]
