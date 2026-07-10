// testpush sends a test push notification to every device registered in the
// pushTokens collection — the same path flowmonitor uses on a real shutoff,
// minus the 20 gallons of water. Run it from any machine with the
// service-account credentials (it doesn't need the Pi):
//
//	cd rpi && go run ./cmd/testpush \
//	  -project watermeter-501022 \
//	  -credentials ../../watermeter-config/config/firebase/service-account.json
package main

import (
	"context"
	"flag"
	"log"

	"cloud.google.com/go/firestore"
	"github.com/anthonywittig/watermeter/watermeter"
	"google.golang.org/api/option"
)

func main() {
	project := flag.String("project", "", "Firebase project id")
	credentials := flag.String("credentials", "", "path to the service-account JSON")
	title := flag.String("title", "Water shut off (test)", "notification title")
	body := flag.String("body", "This is a test of the watermeter shutoff alert.", "notification body")
	flag.Parse()

	if *project == "" || *credentials == "" {
		log.Fatal("-project and -credentials are required")
	}

	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, *project, option.WithCredentialsFile(*credentials))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	notifier, err := watermeter.NewPushNotifier(ctx, fs, *project, *credentials)
	if err != nil {
		log.Fatalf("push notifier: %v", err)
	}

	if err := notifier.NotifyAll(ctx, *title, *body); err != nil {
		log.Fatalf("notify: %v", err)
	}
}
