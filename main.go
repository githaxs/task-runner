package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type Command struct {
	Title         string  `json:"title"`
	Slug          string  `json:"slug"`
	Command       string  `json:"command"`
	Check         bool    `json:"check"`
	FailMessage   string  `json:"fail_message"`
	RunOnFail     bool    `json:"run_on_fail"`
	IncludeOutput bool    `json:"include_output"`
	IncludeInEnv  string  `json:"include_in_env"`
	Completed     bool    `json:"completed"`
	Duration      float64 `json:"duration"`
	ExitCode      int     `json:"exit_code"`
	Output        string  `json:"output"`
}

type Request struct {
	Env      map[string]string `json:"env"`
	Commands []Command         `json:"commands"`
}

type Conclusion string

const (
	ActionRequired Conclusion = "action_required"
	Cancelled      Conclusion = "cancelled"
	Failure        Conclusion = "failure"
	Neutral        Conclusion = "neutral"
	Success        Conclusion = "success"
	Skipped        Conclusion = "skipped"
	Stale          Conclusion = "stale"
	TimedOut       Conclusion = "timed_out"
)

type Response struct {
	Conclusion Conclusion `json:"conclusion"`
	Steps      []Command  `json:"steps"`
	Request    Request    `json:"request"`
}

func (r *Command) Run(env map[string]string) (*Command, error) {
	start := time.Now()
	dir := "/"
	if val, ok := env["DIR"]; ok && r.Slug != "clone" && r.Slug != "git_config" {
		dir = val
	}
	fmt.Printf("Running command: %s in %s\n", r.Command, dir)
	cmd := exec.Command("/bin/sh", "-c", r.Command)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Dir = dir
	stdout, err := cmd.Output()
	if err != nil {
		fmt.Printf("%s", err)
		if exitErr, ok := err.(*exec.ExitError); ok {
			r.ExitCode = exitErr.ExitCode()
			fmt.Printf("%s", exitErr.Stderr)
			if r.ExitCode != 0 {
				r.Output += r.FailMessage
			}
			return r, err
		}
		return r, err
	}

	stop := time.Now()

	fmt.Printf("%s", stdout)
	r.Duration = stop.Sub(start).Seconds()
	r.ExitCode = 0
	r.Completed = true
	fmt.Println(r.IncludeOutput)
	if r.IncludeOutput {
		r.Output += fmt.Sprintf("%s", stdout)
	}
	return r, nil
}

func (t *Request) Run() (bool, []Command) {
	skipNonEssential := false
	var result []Command

	for _, command := range t.Commands {
		fmt.Printf("Running command: %s\n", command.Title)
		if !skipNonEssential || command.RunOnFail {
			step, err := command.Run(t.Env)
			if err != nil {
				fmt.Printf("%s", err)
				step = &Command{
					Title:    command.Title,
					Slug:     command.Slug,
					Command:  command.Command,
					Check:    command.Check,
					Output:   fmt.Sprintf("%s", err),
					ExitCode: 1,
				}
				result = append(result, *step)
			} else {
				result = append(result, *step)
			}

			if step.IncludeInEnv != "" {
				t.Env[step.IncludeInEnv] = step.Output
			}

			if (step.Slug == "terraform_plan" && step.ExitCode != 0 && step.ExitCode != 2) || (step.Slug != "terraform_plan" && step.ExitCode != 0) {
				skipNonEssential = true
			}
		}
	}

	return !skipNonEssential, result
}

func HandleRecord(msg events.SQSMessage) (*Response, error) {
	var request Request
	err := json.Unmarshal([]byte(msg.Body), &request)
	if err != nil {
		return nil, err
	}

	fmt.Printf("%+v\n", request)
	fmt.Printf("Received request: %s\n", msg.Body)

	// Run the commands
	success, steps := request.Run()

	// Build the response
	var conclusion Conclusion
	if success {
		conclusion = Success
	} else {
		conclusion = Failure
	}

	response := Response{
		Conclusion: conclusion,
		Steps:      steps,
		Request:    request,
	}

	return &response, nil
}

func HandleRequest(sqsEvent events.SQSEvent) error {
	fmt.Println("Received event: ", sqsEvent)
	if len(sqsEvent.Records) == 0 {
		return errors.New("No records found in SQS event")
	}

	for _, msg := range sqsEvent.Records {
		// Send the response
		response, err := HandleRecord(msg)
		if err != nil {
			return err
		}

		body, err := json.Marshal(response)
		if err != nil {
			return err
		}
		fmt.Printf("Sending response: %s\n\n", body)

		r, _ := http.NewRequest(http.MethodPost, os.Getenv("RESPONSE_URL"), bytes.NewBuffer(body))
		r.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(r)

		if err != nil {
			return err
		}

		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		fmt.Printf("Response: %s", body)
	}

	return nil
}

func main() {
	lambda.Start(HandleRequest)
}
