package main

import (
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"gopkg.in/h2non/gock.v1"
)

func TestHandleRequest(t *testing.T) {
	// Set up a mock SQS event with one record containing a valid JSON payload
	mockEvent := events.SQSEvent{
		Records: []events.SQSMessage{
			{
				Body: `{"commands": [{"title": "Test Command", "command": "echo 'Hello, World!'", "slug": "test", "include_output": true}]}`,
			},
		},
	}

	// Set up a mock environment variable with a fake response URL
	os.Setenv("RESPONSE_URL", "https://example.com/")

	defer gock.Off()

	gock.New("https://example.com").
		MatchHeader("Accept", "application/json").
		Post("/").
		Reply(200).
		JSON(map[string]string{"value": "fixed"})
	// Call the function and check the result
	err := HandleRequest(mockEvent)

	if err != nil {
		t.Errorf("HandleRequest() returned an error: %s", err)
	}
}

func TestFailureReturnedIfStepFails(t *testing.T) {
	mockEvent := events.SQSMessage{
		Body: `{"commands": [{"title": "Test Command", "command": "echo 'Hello, World!' && exit 1", "slug": "test", "include_output": true}]}`,
	}

	response, err := HandleRecord(mockEvent)

	if err != nil {
		t.Errorf("HandleRecord() returned an error: %s", err)
	}

	if response.Conclusion != Failure {
		t.Errorf("HandleRecord() returned a conclusion of %s, expected %s", response.Conclusion, Failure)
	}
}

func TestEnvUpdatedIfIncludedInEnvSet(t *testing.T) {
	mockEvent := events.SQSMessage{
		Body: `{"commands": [
				{"title": "Test Command", "command": "echo 'Hello, World!'", "slug": "test", "include_output": true, "include_in_env": "TEST_ENV"},
				{"title": "Echo", "command": "echo $TEST_ENV", "slug": "echo", "include_output": true }
			], "env": {"TEST_ENV": "test"}}`,
	}

	response, err := HandleRecord(mockEvent)

	if err != nil {
		t.Errorf("HandleRecord() returned an error: %s", err)
	}

	if response.Conclusion != Success {
		t.Errorf("HandleRecord() returned a conclusion of %s, expected %s", response.Conclusion, Success)
	}

	if response.Steps[0].Output != "Hello, World!\n" {
		t.Errorf("HandleRecord() returned a step output of %s, expected %s", response.Steps[0].Output, "Hello, World!\n")
	}
}
