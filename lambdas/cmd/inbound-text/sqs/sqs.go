package sqs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

type SQSService struct {
	awsAccount string
	awsRegion  string
	c          *sqs.Client
}

func NewSQSService(
	ctx context.Context,
	awsAccount string,
	awsRegion string,
) (*SQSService, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}

	c := sqs.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %s", err.Error())
	}

	return &SQSService{
		awsAccount: awsAccount,
		awsRegion:  awsRegion,
		c:          c,
	}, nil
}

func (s *SQSService) SendMessage(ctx context.Context, level int) error {
	queueURL := fmt.Sprintf(
		"https://sqs.%s.amazonaws.com/%s/watermeter-rpi.fifo",
		s.awsRegion,
		s.awsAccount,
	)

	payload, err := json.Marshal(ValveChangeRequested{
		Level: level,
	})
	if err != nil {
		return fmt.Errorf("error marshaling payload: %s", err.Error())
	}

	input := &sqs.SendMessageInput{
		MessageBody:            aws.String(string(payload)),
		QueueUrl:               aws.String(queueURL),
		MessageGroupId:         aws.String("1"),
		MessageDeduplicationId: aws.String(uuid.New().String()),
	}

	if _, err := s.c.SendMessage(ctx, input); err != nil {
		return fmt.Errorf("error sending sqs message: %s", err.Error())
	}

	return nil
}
