package watermeter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthonywittig/watermeter/watermeter/iot"
	"github.com/anthonywittig/watermeter/watermeter/sqs"
)

type RemoteControl struct {
	sqsService *sqs.SQSService
	valve      *iot.Valve
}

func StartRemoteControl(
	ctx context.Context,
	wg *sync.WaitGroup,
	sqsService *sqs.SQSService,
	valve *iot.Valve,
) error {
	rc := &RemoteControl{
		sqsService: sqsService,
		valve:      valve,
	}

	ticker := time.NewTicker(10 * time.Second).C
	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				fmt.Println("shutting down remote control")
				return
			case <-ticker:
				fmt.Println("remote control tick")
				for {
					processedMessage, err := rc.getAndProcessMessage(ctx)
					if err != nil {
						fmt.Printf("error processing message: %v", err)
					}
					// If we didn't process a message, we should wait for the next tick.
					if !processedMessage {
						break
					}
				}
			}
		}
	}()

	return nil
}

func (rc *RemoteControl) getAndProcessMessage(ctx context.Context) (bool, error) {
	message, err := rc.sqsService.GetMessages(ctx)
	if err != nil {
		fmt.Printf("error getting messages: %v", err)
		return false, nil
	}
	if message == nil {
		return false, nil
	}

	if message.Level <= 0 {
		rc.valve.Close()
		return true, nil
	}
	rc.valve.Open()
	return true, nil
}
