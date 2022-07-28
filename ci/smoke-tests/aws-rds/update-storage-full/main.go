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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	_ "github.com/lib/pq"
)

func randomString(length int32) string {
	s := make([]int32, length)
	for i := int32(0); i < length; i++ {
		s[i] = 'a' + rand.Int31n(25)
	}
	return string(s)
}

func awsStuff(c configuration) {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: &c.s3Region,
	}))
	svc := s3.New(sess)
	if _, err := svc.HeadBucket(&s3.HeadBucketInput{Bucket: &c.s3BucketName}); err != nil {

	}
}

type configuration struct {
	s3AccessKeyID     string
	s3SecretAccessKey string
	s3Region          string
	s3BucketName      string
}

// vcapServicesEnv represents the VCAP_SERVICES environment variable that is set by Cloud Foundry on applications with bound services. The variable changes based on the services bindings made to the app. The fields on vcapServicesEnv correspond to the particular services that must be be bound to this application for it to run.
//
// See also: https://docs.cloudfoundry.org/devguide/deploy-apps/environment-variable.html#VCAP-SERVICES
type vcapServicesEnv struct {
	S3 []struct {
		Credentials struct {
			AccessKeyID     string `json:"access_key_id"`
			SecretAccessKey string `json:"secret_access_key"`
			Region          string `json:"region"`
			Bucket          string `json:"bucket"`
		} `json:"credentials"`
	} `json:"s3"`
}

// credentials reads bound service credentials from the environment.
func credentials() (configuration, error) {
	d := []byte(os.Getenv("VCAP_SERVICES"))
	v := vcapServicesEnv{}
	err := json.Unmarshal(d, &v)
	if err != nil {
		return configuration{}, fmt.Errorf("failed to read required configuration from the environment: %w", err)
	}
	c := configuration{}
	s := v.S3[0]
	c.s3AccessKeyID = s.Credentials.AccessKeyID
	c.s3SecretAccessKey = s.Credentials.SecretAccessKey
	c.s3Region = s.Credentials.Region
	c.s3BucketName = s.Bucket

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
	// check if the s3 bucket already has an object in it.

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()
	// No connection string is required because lib/pq gets standard variables from
	// the environment automatically.
	db, err := sql.Open("postgres", "")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS data(t text);`); err != nil {
		log.Fatal(err)
	}

	for err == nil {
		s := randomString(1024 * 1024) // 1MB per row
		_, err = db.ExecContext(ctx, `INSERT INTO data(t) VALUES ($1);`, s)
	}

	// todo figure out the 'storage full' error and exit 0 if it's that, but otherwise exit 1
	log.Println("Stopped filling database. Error was:", err.Error())

	// here, exec pg_dump (or an aws-specific command?)
	// (or just exit and do this outside?)
	os.Exit(0)
}
