# Smoke Test supporting materials: Update storage when full

This is a small Go app that must be run once before the [update-storage-full](../../../run-smoke-tests-db-update-storage-full.sh) test can be run. The app simply inserts data into a database until it is full. The user must then use `pg_dump` to dump the contents of the database to an s3 bucket for later use by the test.
