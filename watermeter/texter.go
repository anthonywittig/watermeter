package watermeter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Texter struct {
	Account              string
	SID                  string
	Secret               string
	AccountPhoneNumber   string
	RecipientPhoneNumber string
}

func (t *Texter) SendMessage(message string) error {
	msgData := url.Values{}
	msgData.Set("To", t.RecipientPhoneNumber)
	msgData.Set("From", t.AccountPhoneNumber)
	msgData.Set("Body", message)
	msgDataReader := *strings.NewReader(msgData.Encode())

	client := &http.Client{}
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", t.Account)
	req, err := http.NewRequest("POST", apiURL, &msgDataReader)
	if err != nil {
		return err
	}

	req.SetBasicAuth(t.SID, t.Secret)
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
		return errors.New(fmt.Sprintf("bad status code: %s", resp.Status))
	}

	return nil
}
