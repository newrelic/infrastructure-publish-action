package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	surrogateKey    = "infrastructure_metadata"
	fastlySurrogate = "x-amz-meta-surrogate-key"
)

func handler(ctx context.Context, s3Event events.S3Event) error {
	// debugging
	//data, err := json.Marshal(s3Event)
	//if err != nil {
	//	return err
	//}
	//fmt.Printf("%s\f", data)

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)
	if err != nil {
		return fmt.Errorf("error getting s3 session: %w", err)
	}

	s3 := s3.New(sess)
	for _, record := range s3Event.Records {
		s3Record := record.S3
		_, err := s3.CopyObject(modifyMetadata(s3Record))
		if err != nil {
			return fmt.Errorf("error while coping metadata to s3 object: %w", err)
		}
		fmt.Printf("Modified object file %s\n", s3Record.Object.Key)
	}

	fmt.Println("SUCCEED: All S3 Objects metadata updated")
	return nil
}

func modifyMetadata(record events.S3Entity) *s3.CopyObjectInput {
	return &s3.CopyObjectInput{
		Bucket:            aws.String(record.Bucket.Name),
		Key:               aws.String(record.Object.Key),
		CopySource:        aws.String(record.Bucket.Name + "/" + record.Object.Key),
		Metadata:          map[string]*string{fastlySurrogate: aws.String(surrogateKey)},
		MetadataDirective: aws.String("REPLACE"),
	}
}

func main() {
	fmt.Printf("starting aws s3 metadata rewrite tool\n")
	lambda.Start(handler)
}
