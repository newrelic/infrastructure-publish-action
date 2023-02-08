lambda: lambda.go
	GOARCH=amd64 GOOS=linux go build

lambda.zip: lambda
	zip lambda.zip lambda

.PHONY: build
build: lambda lambda.zip

clean:
	rm lambda.zip lambda
