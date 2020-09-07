package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

var httpClient = &http.Client{}

var (
	all         *bool
	id          *string
	ignore      *bool
	database    string
	db          *string
	js          *string
	py          *string
	dsn         DSN
	SENTRY_URL  string
	exists      bool
	projectDSNs map[string]*DSN
	// traceIdMap0 map[string][]*Item
	traceIdMap map[string][]interface{}
	traceIds   []string

	// traceIdMap2 map[string][]string
	// traceIdMap := map[string][]*interface{}
)

type DSN struct {
	host      string
	rawurl    string
	key       string
	projectId string
}

func parseDSN(rawurl string) *DSN {
	fmt.Println("> rawlurl", rawurl)

	// TODO support for http vs. https 7: vs 8:
	key := strings.Split(rawurl, "@")[0][7:]

	uri, err := url.Parse(rawurl)
	if err != nil {
		panic(err)
	}
	idx := strings.LastIndex(uri.Path, "/")
	if idx == -1 {
		log.Fatal("missing projectId in dsn")
	}
	projectId := uri.Path[idx+1:]

	var host string
	if strings.Contains(rawurl, "ingest.sentry.io") {
		host = "ingest.sentry.io"
	}
	if strings.Contains(rawurl, "@localhost:") {
		host = "localhost:9000"
	}
	if host == "" {
		log.Fatal("missing host")
	}
	// if len(key) < 31 || len(key) > 32 {
	// 	log.Fatal("bad key length")
	// }
	if projectId == "" {
		log.Fatal("missing project Id")
	}
	fmt.Printf("> DSN { host: %s, projectId: %s }\n", host, projectId)
	return &DSN{
		host,
		rawurl,
		key,
		projectId,
	}
}

func (d DSN) storeEndpoint() string {
	var fullurl string
	if d.host == "ingest.sentry.io" {
		fullurl = fmt.Sprint("https://", d.host, "/api/", d.projectId, "/store/?sentry_key=", d.key, "&sentry_version=7")
	}
	if d.host == "localhost:9000" {
		fullurl = fmt.Sprint("http://", d.host, "/api/", d.projectId, "/store/?sentry_key=", d.key, "&sentry_version=7")
	}
	if fullurl == "" {
		log.Fatal("problem with fullurl")
	}
	return fullurl
}
func (d DSN) envelopeEndpoint() string {
	var fullurl string
	if d.host == "ingest.sentry.io" {
		fullurl = fmt.Sprint("https://", d.host, "/api/", d.projectId, "/envelope/?sentry_key=", d.key, "&sentry_version=7")
	}
	if d.host == "localhost:9000" {
		fullurl = fmt.Sprint("http://", d.host, "/api/", d.projectId, "/envelope/?sentry_key=", d.key, "&sentry_version=7")
	}
	if fullurl == "" {
		log.Fatal("problem with fullurl")
	}
	return fullurl
}

type Event struct {
	Platform string            `json:"platform"`
	Kind     string            `json:"kind"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"`
}

func (e Event) String() string {
	return fmt.Sprintf("\n Event { Platform: %s, Type: %s }\n", e.Platform, e.Kind) // index somehow?
}

func matchDSN(projectDSNs map[string]*DSN, event Event) string {

	platform := event.Platform

	var storeEndpoint string
	if platform == "javascript" && event.Kind == "error" {
		storeEndpoint = projectDSNs["javascript"].storeEndpoint()
	} else if platform == "python" && event.Kind == "error" {
		storeEndpoint = projectDSNs["python"].storeEndpoint()
	} else if platform == "android" && event.Kind == "error" {
		storeEndpoint = projectDSNs["android"].storeEndpoint()
	} else if platform == "javascript" && event.Kind == "transaction" {
		storeEndpoint = projectDSNs["javascript"].envelopeEndpoint()
	} else if platform == "python" && event.Kind == "transaction" {
		storeEndpoint = projectDSNs["python"].envelopeEndpoint()
	} else if platform == "android" && event.Kind == "transaction" {
		storeEndpoint = projectDSNs["android"].envelopeEndpoint()
	} else {
		log.Fatal("platform type not supported")
	}

	if storeEndpoint == "" {
		log.Fatal("missing store endpoint")
	}
	return storeEndpoint
}

// type Envelope struct {
// 	items []interface{}
// }

// type Item map[string]interface{}

// type Timestamp time.Time
type Timestamp struct {
	time.Time
	rfc3339 bool
}

type Item struct {
	Timestamp Timestamp `json:"timestamp,omitempty"`
	// Timestamp time.Time `json:"timestamp,omitempty"`

	Event_id string `json:"event_id,omitempty"`
	Sent_at  string `json:"sent_at,omitempty"`

	Length       int    `json:"length,omitempty"`
	Type         string `json:"type,omitempty"`
	Content_type string `json:"content_type,omitempty"`

	Start_timestamp string                 `json:"start_timestamp,omitempty"`
	Transaction     string                 `json:"transaction,omitempty"`
	Server_name     string                 `json:"server_name,omitempty"`
	Tags            map[string]interface{} `json:"tags,omitempty"`
	Contexts        map[string]interface{} `json:"contexts,omitempty"`

	Extra       map[string]interface{} `json:"extra,omitempty"`
	Request     map[string]interface{} `json:"request,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	// Todo spans []
	Sdk  map[string]interface{} `json:"sdk,omitempty"`
	User map[string]interface{} `json:"user,omitempty"`
}

// TODO need an ItemFinal that has unified timestamp?

func init() {
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}

	traceIdMap = make(map[string][]interface{})

	all = flag.Bool("all", false, "send all events. default is send latest event")
	id = flag.String("id", "", "id of event in sqlite database") // 08/27 non-functional today
	ignore = flag.Bool("i", false, "ignore sending the event to Sentry.io")
	db = flag.String("db", "", "database.json")
	js = flag.String("js", "", "javascript DSN")
	py = flag.String("py", "", "python DSN")
	flag.Parse()

	// sentry +10.0.0 supports performance monitoring, transactions
	projectDSNs = make(map[string]*DSN)
	projectDSNs["javascript"] = parseDSN(os.Getenv("DSN_JAVASCRIPT_SAAS"))
	if *js != "" {
		projectDSNs["javascript"] = parseDSN(*js)
	}
	projectDSNs["python"] = parseDSN(os.Getenv("DSN_PYTHON_SAAS"))
	if *py != "" {
		projectDSNs["python"] = parseDSN(*py)
	}

	if *db == "" {
		database = os.Getenv("JSON")
	} else {
		database = *db
	}
}

func main() {
	jsonFile, err := os.Open(database)
	if err != nil {
		log.Fatal(err)
	}

	byteValue, _ := ioutil.ReadAll(jsonFile)
	defer jsonFile.Close()
	events := make([]Event, 0)
	if err := json.Unmarshal(byteValue, &events); err != nil {
		panic(err)
	}
	requests := []Transport{}
	for _, event := range events {
		fmt.Printf("\n> KIND|PLATFORM %v %v ", event.Kind, event.Platform)

		// X-Sentry-Trace and py/js errors have a trace_id too
		// can do errors and transactions separate from each other?

		if event.Kind == "error" {
			bodyError, timestamper, bodyEncoder, storeEndpoint := decodeError(event)
			bodyError = eventId(bodyError)
			bodyError = release(bodyError)
			bodyError = user(bodyError)
			bodyError = timestamper(bodyError, event.Platform)

			requests = append(requests, Transport{
				kind:         event.Kind,
				platform:     event.Platform,
				eventHeaders: event.Headers,

				bodyError:     bodyError,
				storeEndpoint: storeEndpoint,
				bodyEncoder:   bodyEncoder,
			})

		} else if event.Kind == "transaction" {
			envelopeItems, envelopeTimestamper, envelopeEncoder, storeEndpoint := decodeEnvelope(event)
			envelopeItems = eventIds(envelopeItems)
			envelopeItems = envelopeTimestamper(envelopeItems, event.Platform)
			envelopeItems = envelopeReleases(envelopeItems, event.Platform, event.Kind)
			envelopeItems = removeLengthField(envelopeItems)

			// fills global traceIdMap
			getEnvelopeTraceIds(envelopeItems)

			requests = append(requests, Transport{
				kind:         event.Kind,
				platform:     event.Platform,
				eventHeaders: event.Headers,
				//event: Event

				envelopeItems:   envelopeItems,
				storeEndpoint:   storeEndpoint,
				envelopeEncoder: envelopeEncoder,
			})
		}

	}
	fmt.Println("\n > REQUESTS transport length", len(requests))

	setEnvelopeTraceIds(requests)
	encodeAndSendEvents(requests, *ignore)

	return
}

// TODO envelopeItems is diff after each iteration.... so need 1 global array for ALL events' envelope items...? revisit the
// traceIdMap in here:

// "factory"
// 2. Update All Envelopes with proper Trace Id - setEnvelopeTraceIds
// 3. Send all to Sentry.io
// build all requests, or have that done ahead of time?

// requestHub.add(error_or_transaction?)
// requestHubError.add
// requestHubTransaction.add
