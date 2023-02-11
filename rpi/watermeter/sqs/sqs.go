package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type SQSService struct {
	c *sqs.Client
}

func NewSQSService(ctx context.Context) (*SQSService, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile("water-meter-rpi"))
	if err != nil {
		return nil, fmt.Errorf("error getting aws config: %s", err.Error())
	}

	c := sqs.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %s", err.Error())
	}

	return &SQSService{
		c: c,
	}, nil
}

func (s *SQSService) GetMessages(ctx context.Context) (*ValveChangeRequested, error) {
	queueName := "watermeter-rpi.fifo"

	if !strings.HasSuffix(queueName, ".fifo") {
		return nil, fmt.Errorf("queue name must end in .fifo")
	}

	resp, err := s.c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(queueName),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting sqs messages: %s", err.Error())
	}

	if len(resp.Messages) == 0 {
		return nil, nil
	}
	if len(resp.Messages) > 1 {
		return nil, fmt.Errorf("expected 1 message, got %d", len(resp.Messages))
	}

	message := resp.Messages[0]

	// Assume success and delete the message.
	_, err = s.c.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueName),
		ReceiptHandle: message.ReceiptHandle,
	})
	if err != nil {
		return nil, fmt.Errorf("error deleting sqs message: %s", err.Error())
	}

	m := &ValveChangeRequested{}
	if err := json.Unmarshal([]byte(*message.Body), m); err != nil {
		return nil, fmt.Errorf("error unmarshalling sqs message: %s", err.Error())
	}

	return m, nil

}
