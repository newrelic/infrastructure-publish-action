ARG VERSION=1.15.7-buster
FROM golang:$VERSION

# Args
ARG AWS_S3_MOUNT_DIRECTORY=/mnt/s3

# Tools
# validate s3fs-fuse with the sec team
RUN apt-get -qq update && apt-get -qq install -y \
    make \
    curl \
    gnupg2 \
    bzip2 \
    createrepo \
    unzip

# Sadly installing aptly with apt lead you to a old version not supporting gpg2
WORKDIR /tmp
RUN wget -q https://github.com/aptly-dev/aptly/releases/download/v1.4.0/aptly_1.4.0_linux_amd64.tar.gz
RUN tar xzf aptly_1.4.0_linux_amd64.tar.gz
RUN mv ./aptly_1.4.0_linux_amd64/aptly /usr/bin/aptly


RUN wget -q https://github.com/kahing/goofys/releases/download/v0.24.0/goofys -O /usr/bin/goofys
RUN chmod +x /usr/bin/goofys

RUN curl -s "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
RUN unzip -q awscliv2.zip
RUN ./aws/install

# Prepare action
WORKDIR /home/gha/publisher
ADD publisher .
RUN go build -o /bin/publisher ./publisher.go
RUN chmod +x /bin/publisher

WORKDIR /home/gha
ADD schemas ./schemas
ADD scripts/Makefile .
RUN mkdir ./assets
RUN mkdir $AWS_S3_MOUNT_DIRECTORY
ENV AWS_S3_MOUNT_DIRECTORY $AWS_S3_MOUNT_DIRECTORY

# Run action
CMD ["make", "--jobs=1"]
