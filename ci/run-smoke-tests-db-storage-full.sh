#!/bin/bash

# todo replace all instances of

set -euxo pipefail

# Function for waiting on a service instance to finish being processed.
wait_for_service_instance() {
  local service_name=$1
  local guid=$(cf service --guid $service_name)
  local status=$(cf curl /v2/service_instances/${guid} | jq -r '.entity.last_operation.state')

  while [ "$status" == "in progress" ]; do
    sleep 60
    status=$(cf curl /v2/service_instances/${guid} | jq -r '.entity.last_operation.state')
  done
}

# todo (mxplusb): update the auth mechanism.
cf login -a "$CF_API_URL" -u "$CF_USERNAME" -p "$CF_PASSWORD" -o "$CF_ORGANIZATION" -s "$CF_SPACE"

# Clean up existing app and service if present
cf delete -f "smoke-tests-storage-full-$SERVICE_PLAN"
cf delete-service -f "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"

# change into the directory and push the app without starting it.
pushd aws-db-test/databases/aws-rds
cf push "smoke-tests-storage-full-${SERVICE_PLAN}" -f manifest.yml --no-start

# set some variables that it needs
cf set-env "smoke-tests-storage-full-${SERVICE_PLAN}" DB_TYPE "${SERVICE_PLAN}"
cf set-env "smoke-tests-storage-full-${SERVICE_PLAN}" SERVICE_NAME "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"

# Create service
if echo "$SERVICE_PLAN" | grep -v shared | grep mysql >/dev/null ; then
  # test out the enable_functions stuff, which only works on non-shared mysql databases
  cf create-service aws-rds "$SERVICE_PLAN" "rds-smoke-tests-db-storage-full-$SERVICE_PLAN" -b "$BROKER_NAME" -c '{"enable_functions": true}'
else
  # create a regular instance
  cf create-service aws-rds "$SERVICE_PLAN" "rds-smoke-tests-db-storage-full-$SERVICE_PLAN" -b "$BROKER_NAME"
fi

while true; do
  if out=$(cf bind-service "smoke-tests-storage-full-${SERVICE_PLAN}" "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"); then
    break
  fi
  if [[ $out =~ "Instance not available yet" ]]; then
    echo "${out}"
  fi
  sleep 90
done

# wait for the app to start. if the app starts, it's passed the smoke test.
cf push "smoke-tests-storage-full-${SERVICE_PLAN}"

# push the database filling app
popd
pushd smoke-tests/aws-rds/update-storage-full
cf push "aws-broker-smoke-tests-fill-storage" -f manifest.yml --no-start
cf bind-service "aws-broker-smoke-tests-fill-storage" "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"
cf push "aws-broker-smoke-tests-fill-storage"

url=$(cf app "aws-broker-smoke-tests-fill-storage" | grep routes | awk '{print $2}')

# repeatedly check if the database is full.
while true; do
  if curl "${url}/db" | grep -q true; then
    break
  fi
done

# increase instance storage.
cf update-service "rds-smoke-tests-db-storage-full-$SERVICE_PLAN" -c '{"storage": 12}'

# Wait to make sure that the service instance has been successfully updated.
wait_for_service_instance "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"

# Clean up app and service.
cf delete -f "smoke-tests-storage-full-$SERVICE_PLAN"
cf delete -f "aws-broker-smoke-tests-fill-storage"
cf delete-service -f "rds-smoke-tests-db-storage-full-$SERVICE_PLAN"
