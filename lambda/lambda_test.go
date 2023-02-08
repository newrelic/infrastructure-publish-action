package main

import (
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLambdaHandler(t *testing.T) {
	testData := `
{
  "Records": [
    {
      "eventVersion": "2.0",
      "eventSource": "aws:s3",
      "awsRegion": "us-east-1",
      "eventTime": "1970-01-01T00:00:00.123Z",
      "eventName": "ObjectCreated:Put",
      "userIdentity": {
        "principalId": "EXAMPLE"
      },
      "requestParameters": {
        "sourceIPAddress": "127.0.0.1"
      },
      "responseElements": {
        "x-amz-request-id": "C3D13FC810",
        "x-amz-id-2": "FMyUY8/IgAtTv8V5Wp6S7S/JRWeUWerMOjpD"
      },
      "s3": {
        "s3SchemaVersion": "1.0",
        "configurationId": "testCigRule",
        "bucket": {
          "name": "sourcebucket",
          "ownerIdentity": {
            "principalId": "EXAMPLE"
          },
          "arn": "arn:aws:s3:::mybucket"
        },
        "object": {
          "key": "Happy%20Face.jpg",
          "size": 1024,
          "versionId": "version",
          "eTag": "d41d8cd98f00be",
          "sequencer": "Happy Sequencer"
        }
      }
    }
  ]
}
`
	eventData := events.S3Event{}
	err := json.Unmarshal([]byte(testData), &eventData)
	assert.NoError(t, err)
	expectedObject := s3.CopyObjectInput{
		Bucket:            aws.String("sourcebucket"),
		Key:               aws.String("Happy%20Face.jpg"),
		CopySource:        aws.String("sourcebucket/Happy%20Face.jpg"),
		Metadata:          map[string]*string{"x-amz-meta-surrogate-key": aws.String("test_1")},
		MetadataDirective: aws.String("REPLACE"),
	}
	assert.Equal(t, expectedObject, *modifyMetadata(eventData.Records[0].S3))
}
