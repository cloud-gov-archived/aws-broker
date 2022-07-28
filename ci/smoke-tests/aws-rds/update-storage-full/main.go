package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	// init the postgres driver
	_ "github.com/lib/pq"
)

var databaseFull bool
var records int64

func randomString(length int32) string {
	s := make([]int32, length)
	for i := int32(0); i < length; i++ {
		s[i] = 'a' + rand.Int31n(25)
	}
	return string(s)
}

type configuration struct {
	databaseURI string
}

// vcapServicesEnv represents a subset of the VCAP_SERVICES environment variable that is set by Cloud Foundry on applications with bound services. The variable holds a JSON blob and its contents vary depending on the service bindings made to the app. The fields listed here correspond to the particular services that must be be bound to this application for it to run.
//
// See also: https://docs.cloudfoundry.org/devguide/deploy-apps/environment-variable.html#VCAP-SERVICES
type vcapServicesEnv struct {
	RDS []struct {
		Credentials struct {
			URI string `json:"uri"`
		} `json:"credentials"`
	} `json:"aws-rds"`
}

// config reads configuration from the environment.
func config(ctx context.Context, timeout time.Duration) (configuration, error) {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(timeout))
	defer cancel()

	s := os.Getenv("VCAP_SERVICES")
	// wait for service binding information to appear until deadline is exceeded.
	for s == "" {
		select {
		case <-ctx.Done():
			return configuration{}, fmt.Errorf("service binding information was not found before timeout")
		default:
		}
		time.Sleep(time.Second)
		s = os.Getenv("VCAP_SERVICES")
	}

	v := vcapServicesEnv{}
	err := json.Unmarshal([]byte(s), &v)
	if err != nil {
		return configuration{}, fmt.Errorf("failed to read required configuration from the environment: %w", err)
	}
	c := configuration{}
	c.databaseURI = v.RDS[0].Credentials.URI

	return c, nil
}

func fillDatabase(ctx context.Context, db *sql.DB) {
	var err error
	if _, err := db.Exec(`DROP TABLE IF EXISTS data; CREATE TABLE data(t text);`); err != nil {
		log.Fatal("dropping and recreating table", err)
	}

	for err == nil {
		s := randomString(1024 * 1024) // 1MB per row
		_, err = db.ExecContext(ctx, `INSERT INTO data(t) VALUES ($1);`, s)
		records++
	}

	if strings.Contains(err.Error(), "No space left on device") {
		databaseFull = true
		log.Printf("Stopped filling database. Filled %v rows. Error was: %v\n", records, err.Error())
	}
}

func logUpdates(ctx context.Context, db *sql.DB) {
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case t := <-t.C:
			log.Printf("time: %v. records inserted: %v.", t, records)
		case <-ctx.Done():
			return
		}
	}
}

func newDB(ctx context.Context) *sql.DB {
	c, err := config(ctx, 30*time.Second)
	if err != nil {
		log.Fatal("loading configuration:", err)
	}
	db, err := sql.Open("postgres", c.databaseURI)
	if err != nil {
		log.Fatal("opening connection:", err)
	}
	return db
}

// main populates a database with random data until it is full, and serves an endpoint that indicates whether the database has been filled.
//
// Standard postgresql connection variables must be set on the environment for
// this script to run. See https://www.postgresql.org/docs/14/libpq-envars.html.
func main() {
	// Set an overall timeout for the insert operation.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(16*time.Minute))
	defer cancel()

	db := newDB(ctx)
	defer db.Close()
	go fillDatabase(ctx, db)
	go logUpdates(ctx, db)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("cloud foundry status check OK\n"))
	})
	http.HandleFunc("/db", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("%v\n", databaseFull)))
	})
	fmt.Println(http.ListenAndServe(":8080", nil))
}
