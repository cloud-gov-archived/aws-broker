package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	// init the postgres driver
	_ "github.com/lib/pq"
)

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

// main populates a database with random data until it is full, then dumps the
// database contents to an s3 bucket for later use in smoke tests. It does not
// run with the smoke tests; it should only need to be re-run if the testdata
// bucket or its contents are deleted.
//
// Standard postgresql connection variables must be set on the environment for
// this script to run. See https://www.postgresql.org/docs/14/libpq-envars.html.
func main() {
	// 10 minute overall timeout.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Minute))
	defer cancel()

	c, err := config(ctx, 30*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("postgres", c.databaseURI)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS data(t text);`); err != nil {
		log.Fatal(err)
	}

	var rows int64
	for err == nil {
		s := randomString(1024 * 1024) // 1MB per row
		_, err = db.ExecContext(ctx, `INSERT INTO data(t) VALUES ($1);`, s)
		rows++
	}

	// todo figure out the 'storage full' error and exit 0 if it's that, but otherwise exit 1
	log.Printf("Stopped filling database. Filled %v rows. Error was: %v\n", rows, err.Error())

	os.Exit(0)
}
