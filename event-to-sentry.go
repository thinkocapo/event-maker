package main

import (
	_ "github.com/mattn/go-sqlite3"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	// "github.com/buger/jsonparser"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{} // CheckRedirect: redirectPolicyFunc,

var (
	all *bool
	db *sql.DB
	DSN, SENTRY_URL string 
	exists bool
)

// TODO could []byte force-type headers into bytes? wouldn't have to []byte(event.headers) later
// initializer function here too much work?
type Event struct {
	id int
	name, _type string
	headers []byte
	bodyBytesCompressed []byte
}

func init() {
	if err := godotenv.Load(); err != nil {
        log.Print("No .env file found")
	}
	DSN, exists = os.LookupEnv("DSN")
	if !exists || DSN=="" { 
		log.Fatal("MISSING DSN")
	}
	fmt.Println("> DSN", DSN)
	KEY := strings.Split(DSN, "@")[0][7:]
	PROJECT_ID := DSN[len(DSN)-1:]
	SENTRY_URL = strings.Join([]string{"http://localhost:9000/api/",PROJECT_ID,"/store/?sentry_key=",KEY,"&sentry_version=7"}, "")

	all = flag.Bool("all", false, "send all events or 1 event from database")
	flag.Parse()
	fmt.Printf("> --all= %v\n", *all)
	
	db, _ = sql.Open("sqlite3", "sqlite.db")
}

func main() {
	rows, err := db.Query("SELECT * FROM events")
	if err != nil {
		fmt.Println("Failed to load rows", err)
	}
	for rows.Next() {
		var event Event
		rows.Scan(&event.id, &event.name, &event._type, &event.bodyBytesCompressed, &event.headers)

		bodyBytes := decodeGzip(event.bodyBytesCompressed)
		bodyInterface := unmarshalJSON(bodyBytes)

		bodyInterface = replaceEventId(bodyInterface)
		bodyInterface = replaceTimestamp(bodyInterface)
		
		bodyBytesPost := marshalJSON(bodyInterface)
		buf := encodeGzip(bodyBytesPost)

		request, errNewRequest := http.NewRequest("POST", SENTRY_URL, &buf)
		if errNewRequest != nil { log.Fatalln(errNewRequest) }

		headerInterface := unmarshalJSON(event.headers)

		for _, v := range [6]string{"Host", "Accept-Encoding","Content-Length","Content-Encoding","Content-Type","User-Agent"} {
			request.Header.Set(v, headerInterface[v].(string))
		}

		response, requestErr := httpClient.Do(request)
		if requestErr != nil { fmt.Println(requestErr) }

		responseData, responseDataErr := ioutil.ReadAll(response.Body)
		if responseDataErr != nil { log.Fatal(responseDataErr) }

		fmt.Printf("> event %v\n", string(responseData))

		if !*all {
			rows.Close()
		}
	}
	rows.Close()
}

func decodeGzip(bodyBytesInput []byte) (bodyBytesOutput []byte) {
	bodyReader, err := gzip.NewReader(bytes.NewReader(bodyBytesInput))
	if err != nil {
		fmt.Println(err)
	}
	bodyBytesOutput, err = ioutil.ReadAll(bodyReader)
	if err != nil {
		fmt.Println(err)
	}
	return
}

func encodeGzip(b []byte) bytes.Buffer {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	// return buf.Bytes()
	return buf
}

func unmarshalJSON(bytes []byte) map[string]interface{} {
	var _interface map[string]interface{}
	if err := json.Unmarshal(bytes, &_interface); err != nil {
		panic(err)
	}
	return _interface
}

func marshalJSON(bodyInterface map[string]interface{}) []byte {
	bodyBytes, errBodyBytes := json.Marshal(bodyInterface) 
	if errBodyBytes != nil { fmt.Println(errBodyBytes)}
	return bodyBytes
}

func replaceEventId(bodyInterface map[string]interface{}) map[string]interface{} {
	fmt.Println("before",bodyInterface["event_id"])
	var uuid4 = strings.ReplaceAll(uuid.New().String(), "-", "") 
	bodyInterface["event_id"] = uuid4
	fmt.Println("after ",bodyInterface["event_id"])
	return bodyInterface
}

func replaceTimestamp(bodyInterface map[string]interface{}) map[string]interface{} {
	fmt.Println("before",bodyInterface["timestamp"])
	timestamp := time.Now()
	oldTimestamp := bodyInterface["timestamp"].(string)
	newTimestamp := timestamp.Format("2006-01-02") + "T" + timestamp.Format("15:04:05")
	bodyInterface["timestamp"] = newTimestamp + oldTimestamp[19:]
	fmt.Println("after ",bodyInterface["timestamp"])
	return bodyInterface
}