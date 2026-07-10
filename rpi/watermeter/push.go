package watermeter

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// PushNotifier sends FCM push notifications to every device registered in the
// Firestore `pushTokens` collection (doc id == the FCM token, written by the
// PWA). Tokens FCM reports as dead are pruned so the registry stays clean.
type PushNotifier struct {
	fs  *firestore.Client
	msg *messaging.Client
}

func NewPushNotifier(
	ctx context.Context,
	fs *firestore.Client,
	projectID string,
	credentialsFile string,
) (*PushNotifier, error) {
	app, err := firebase.NewApp(
		ctx,
		&firebase.Config{ProjectID: projectID},
		option.WithCredentialsFile(credentialsFile),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating firebase app: %w", err)
	}

	msg, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating messaging client: %w", err)
	}

	return &PushNotifier{
		fs:  fs,
		msg: msg,
	}, nil
}

// NotifyAll sends a data-only message (the PWA's service worker displays it)
// to every registered device. Failures on individual tokens don't stop the
// fan-out; dead tokens are deleted.
func (p *PushNotifier) NotifyAll(ctx context.Context, title string, body string) error {
	iter := p.fs.Collection("pushTokens").Documents(ctx)
	defer iter.Stop()

	sent := 0
	for {
		docSnap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("error listing push tokens: %w", err)
		}

		token := docSnap.Ref.ID
		_, err = p.msg.Send(ctx, &messaging.Message{
			Token: token,
			Data: map[string]string{
				"title": title,
				"body":  body,
			},
			Webpush: &messaging.WebpushConfig{
				Headers: map[string]string{
					"Urgency": "high",
					"TTL":     "86400", // deliver up to a day late rather than drop
				},
			},
		})
		if err != nil {
			if messaging.IsRegistrationTokenNotRegistered(err) {
				log.Printf("pruning dead push token %s...", token[:min(12, len(token))])
				if _, err := docSnap.Ref.Delete(ctx); err != nil {
					log.Printf("error deleting dead push token: %v", err)
				}
			} else {
				log.Printf("error sending push: %v", err)
			}
			continue
		}
		sent++
	}

	fmt.Printf("push: sent %q to %d device(s)\n", title, sent)
	return nil
}
