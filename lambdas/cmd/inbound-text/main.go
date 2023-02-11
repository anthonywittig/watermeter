package main

import (
	"context"
	"encoding/json"
	"fmt"
	"lambdas/cmd/inbound-text/sqs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/twilio/twilio-go/client"
)

type Env struct {
	AllowedPhoneNumbers []string `json:"allowedPhoneNumbers"`
	AWSAccount          string   `json:"awsAccount"`
	AWSRegion           string   `json:"awsRegion"`
	TwilioAccountNumber string   `json:"twilioAccountNumber"`
	TwilioAuthToken     string   `json:"twilioAuthToken"`
	TwilioSID           string   `json:"twilioSid"`
	TwilioSecret        string   `json:"twilioSecret"`
}

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	if request.RequestContext.HTTP.Method == "GET" {
		if err := get(ctx, request); err != nil {
			fmt.Printf("error processing GET: %s\n", err)
			return events.LambdaFunctionURLResponse{
				StatusCode: 500,
				Headers: map[string]string{
					"Content-Type": "text/xml",
				},
			}, nil
		}

		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Headers: map[string]string{
				"Content-Type": "text/xml",
			},
		}, nil
	}
	return events.LambdaFunctionURLResponse{Body: "Unexpected", StatusCode: 400}, nil
}

func get(ctx context.Context, request events.LambdaFunctionURLRequest) error {
	b, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("error marshaling request: %w", err)
	}
	fmt.Printf("request: %s\n", string(b))

	envJsonString, ok := os.LookupEnv("ENV")
	if !ok {
		return fmt.Errorf("env not found")
	}
	var env Env
	if err := json.Unmarshal([]byte(envJsonString), &env); err != nil {
		return fmt.Errorf("error unmarshaling ENV: %w", err)
	}

	signature, ok := request.Headers["x-twilio-signature"]
	if !ok {
		return fmt.Errorf("missing x-twilio-signature header")
	}

	requestValidator := client.NewRequestValidator(env.TwilioAuthToken)
	if valid := requestValidator.ValidateBody(
		fmt.Sprintf("https://%s/?%s", request.RequestContext.DomainName, request.RawQueryString),
		[]byte{},
		signature,
	); !valid {
		return fmt.Errorf("invalid signature")
	}

	from, ok := request.QueryStringParameters["From"]
	if !ok {
		return fmt.Errorf("missing From parameter")
	}

	found := false
	for _, allowedPhoneNumber := range env.AllowedPhoneNumbers {
		if from == allowedPhoneNumber {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("from not in allowed list: %s\n", from)
		return nil
	}

	to, ok := request.QueryStringParameters["To"]
	if !ok {
		return fmt.Errorf("missing To parameter")
	}

	message, ok := request.QueryStringParameters["Body"]
	if !ok {
		return fmt.Errorf("missing Body query parameter")
	}
	fmt.Printf("looks good: %s\n", message)

	replyMessage := ""
	intMessage, err := strconv.Atoi(message)
	if err != nil {
		replyMessage = "doesn't look like you sent a number, try a 0-10"
	} else {
		sqsService, err := sqs.NewSQSService(ctx, env.AWSAccount, env.AWSRegion)
		if err != nil {
			return fmt.Errorf("error creating sqs service: %w", err)
		}
		if err := sqsService.SendMessage(ctx, intMessage); err != nil {
			return fmt.Errorf("error sending message: %w", err)
		}
		replyMessage = fmt.Sprintf("passed \"%d\" on to the queue", intMessage)
	}

	// We swap the to/from since we're sending a reply.
	if err := sendSMS(env, from, to, replyMessage); err != nil {
		return fmt.Errorf("error sending sms: %w", err)
	}

	return nil
}

func sendSMS(env Env, to string, from string, message string) error {
	msgData := url.Values{}
	msgData.Set("To", to)
	msgData.Set("From", from)
	msgData.Set("Body", message)
	msgDataReader := *strings.NewReader(msgData.Encode())

	client := &http.Client{}
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", env.TwilioAccountNumber)
	req, err := http.NewRequest("POST", apiURL, &msgDataReader)
	if err != nil {
		return err
	}

	req.SetBasicAuth(env.TwilioSID, env.TwilioSecret)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var data map[string]interface{}
		decoder := json.NewDecoder(resp.Body)
		err := decoder.Decode(&data)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("bad status code: %s - %+v", resp.Status, resp)
	}

	return nil
}
