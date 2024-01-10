package redis

import (
	"testing"

	"github.com/18F/aws-broker/catalog"
	"github.com/18F/aws-broker/config"
	"github.com/18F/aws-broker/helpers"
	"github.com/go-test/deep"
)

func TestInitInstanceTags(t *testing.T) {
	plan := catalog.RedisPlan{
		Tags: map[string]string{
			"plan-tag-1": "foo",
		},
	}
	tags := map[string]string{
		"tag-1": "bar",
	}

	instance := &RedisInstance{}
	instance.init(
		"uuid-1",
		"org-1",
		"space-1",
		"service-1",
		plan,
		RedisOptions{},
		&config.Settings{
			EncryptionKey: helpers.RandStr(16),
		},
		tags,
	)

	expectedTags := map[string]string{
		"plan-tag-1": "foo",
		"tag-1":      "bar",
	}

	if diff := deep.Equal(instance.Tags, expectedTags); diff != nil {
		t.Error(diff)
	"reflect"
	"testing"
)

func TestModifyInstance(t *testing.T) {
	testCases := map[string]struct {
		options          RedisOptions
		existingInstance *RedisInstance
		expectedInstance *RedisInstance
		expectErr        bool
	}{
		"update engine version": {
			options: RedisOptions{
				EngineVersion: "7.0",
			},
			existingInstance: &RedisInstance{
				EngineVersion: "6.0",
			},
			expectedInstance: &RedisInstance{
				EngineVersion: "7.0",
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			err := test.existingInstance.modify(test.options)
			if !test.expectErr && err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if test.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !reflect.DeepEqual(test.existingInstance, test.expectedInstance) {
				t.Fatalf("expected instance: %+v, got instance: %+v", test.expectedInstance, test.existingInstance)
			}
		})
	}
}
