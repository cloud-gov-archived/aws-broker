package redis

import (
	"errors"
	"reflect"
	"testing"

	"github.com/18F/aws-broker/catalog"
	"github.com/18F/aws-broker/config"
	"github.com/18F/aws-broker/helpers/request"
)

func TestValidate(t *testing.T) {
	testCases := map[string]struct {
		options     RedisOptions
		plan        catalog.RedisPlan
		expectedErr bool
	}{
		"invalid engine version": {
			options: RedisOptions{
				EngineVersion: "4.1",
			},
			plan: catalog.RedisPlan{
				ApprovedMajorVersions: []string{"5.0"},
			},
			expectedErr: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			err := test.options.Validate(test.plan)
			if test.expectedErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.expectedErr && err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
		})
	}
}

func TestParseOptionsFromRequest(t *testing.T) {
	testCases := map[string]struct {
		broker          *redisBroker
		request         request.Request
		expectedOptions RedisOptions
		plan            catalog.RedisPlan
		expectedErr     error
	}{
		"nothing specified": {
			broker: &redisBroker{
				settings: &config.Settings{},
			},
			request: request.Request{
				RawParameters: []byte(``),
			},
			expectedOptions: RedisOptions{},
		},
		"supported engine version specified": {
			broker: &redisBroker{
				settings: &config.Settings{},
			},
			request: request.Request{
				RawParameters: []byte(`{"engine_version": "7.0"}`),
			},
			plan: catalog.RedisPlan{
				ApprovedMajorVersions: []string{"7.0"},
			},
			expectedOptions: RedisOptions{},
		},
		"unsupported engine version specified": {
			broker: &redisBroker{
				settings: &config.Settings{},
			},
			request: request.Request{
				RawParameters: []byte(`{"engineVersion": "9.0"}`),
			},
			plan: catalog.RedisPlan{
				ApprovedMajorVersions: []string{"7.0", "8.0"},
			},
			expectedErr: errors.New("9.0 is not a supported major version; major version must be one of: 7.0, 8.0"),
			expectedOptions: RedisOptions{
				EngineVersion: "9.0",
			},
		},
	}
	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			options, err := test.broker.parseOptionsFromRequest(test.request, test.plan)
			if test.expectedErr == nil && err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if test.expectedErr != nil && err.Error() != test.expectedErr.Error() {
				t.Fatalf("expected error: %s, got: %s", test.expectedErr, err)
			}
			if !reflect.DeepEqual(test.expectedOptions, options) {
				t.Errorf("expected: %+v, got %+v", test.expectedOptions, options)
			}
		})
	}
}
