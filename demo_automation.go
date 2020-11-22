package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"github.com/getsentry/sentry-go"
	"google.golang.org/api/iterator"
)

type DemoAutomation struct{}

const JAVASCRIPT = "javascript"
const PYTHON = "python"

func (d *DemoAutomation) downloadEvents() []Event {
	org := os.Getenv("ORG")
	var eventIds []string

	// Call Sentry w/ 24HrPeriod events with Projects selected
	endpoint := "https://sentry.io/api/0/organizations/testorg-az/eventsv2/?statsPeriod=24h&project=5260888&project=1428657&field=title&field=event.type&field=project&field=user.display&field=timestamp&sort=-timestamp&per_page=50&query="
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalln(err)
	}
	request.Header.Set("content-type", "application/json")
	var httpClient = &http.Client{}
	response, requestErr := httpClient.Do(request)
	if requestErr != nil {
		sentry.CaptureException(requestErr)
		log.Fatal(requestErr)
	}
	body, errResponse := ioutil.ReadAll(response.Body)
	if errResponse != nil {
		sentry.CaptureException(errResponse)
		log.Fatal(errResponse)
	}

	// TODO
	var q Query
	err = json.Unmarshal(body, &q)
	// responseData []byte into []interface{} (only need eventId, no need to Type check everything)
	for _, event := range responseData {
		eventId := event.(map[string]interface{})["eventId"]
		eventIds = append(eventIds, eventId)
	}

	for _, id := range eventIds {
		// 2. Call JSON URL for each
		// http https://sentry.io/api/0/projects/testorg-az/will-frontend-react/events/e65817084e5b4af19fe3005d7c536e84/json/

		// 3.
		byteValue, _ := ioutil.ReadAll(somethingThatReadSentry)
		var event Event
		if err := json.Unmarshal(byteValue, &event); err != nil {
			sentry.CaptureException(err)
			panic(err)
		}
		event.setDsn()
		events = append(events, event)
	}

	var events []Event
	return events
}

func (d *DemoAutomation) getEvents(prefix string) []Event {
	// Initialize/Connect the Client
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalln("storage.NewClient:", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, time.Second*50)
	defer cancel()

	// Get the bucket and its file names
	bucketName := os.Getenv("BUCKET")
	bucketHandle := client.Bucket(bucketName)

	var fileNames []string

	query := &storage.Query{Prefix: prefix}
	it := bucketHandle.Objects(ctx, query)
	for {
		obj, err := it.Next()
		if err == iterator.Done {
			sentry.CaptureMessage(fmt.Sprintf("finished retrieving %v file names", len(fileNames)))
			break
		}
		if err != nil {
			sentry.CaptureException(err)
			log.Fatalln("listBucket: unable to list bucket", err)
		}
		fileNames = append(fileNames, obj.Name)
		print(obj)
	}

	// Get the files
	var events []Event
	for _, fileName := range fileNames {
		rc, err := bucketHandle.Object(fileName).NewReader(ctx)
		if err != nil {
			sentry.CaptureException(err)
			log.Fatalln("NewReader:", err)
		}
		byteValue, _ := ioutil.ReadAll(rc)

		// Dev Note - The Event's UnmarshalJSON method is overriden in Event.go
		var event Event
		if err := json.Unmarshal(byteValue, &event); err != nil {
			sentry.CaptureException(err)
			panic(err)
		}
		event.setDsn()
		events = append(events, event)
	}
	return events
}

func print(obj *storage.ObjectAttrs) {
	fmt.Printf("filename: /%v/%v \n", obj.Bucket, obj.Name) // .ContentType .Owner .Size
}
