package redis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"code.cloudfoundry.org/lager"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticache"
	brokertags "github.com/cloud-gov/go-broker-tags"
	"github.com/jinzhu/gorm"

	"github.com/18F/aws-broker/base"
	"github.com/18F/aws-broker/catalog"
	"github.com/18F/aws-broker/config"
	"github.com/18F/aws-broker/helpers/request"
	"github.com/18F/aws-broker/helpers/response"
)

type RedisOptions struct {
	EngineVersion string `json:"engineVersion"`
}

func (r RedisOptions) Validate(plan catalog.RedisPlan) error {
	// Check to make sure that the version specified is allowed by the plan.
	if r.EngineVersion != "" {
		if !plan.CheckVersion(r.EngineVersion) {
			return fmt.Errorf("%s is not a supported major version; major version must be one of: 7.0, 6.2, 6.0, 5.0.6", r.EngineVersion)
		}
	}
	return nil
}

type redisBroker struct {
	brokerDB   *gorm.DB
	settings   *config.Settings
	logger     lager.Logger
	tagManager brokertags.TagManager
}

// InitRedisBroker is the constructor for the redisBroker.
func InitRedisBroker(
	brokerDB *gorm.DB,
	settings *config.Settings,
	tagManager brokertags.TagManager,
) base.Broker {
	logger := lager.NewLogger("aws-redis-broker")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.INFO))
	return &redisBroker{brokerDB, settings, logger, tagManager}
}

// this helps the manager to respond appropriately depending on whether a service/plan needs an operation to be async
func (broker *redisBroker) AsyncOperationRequired(c *catalog.Catalog, i base.Instance, o base.Operation) bool {
	switch o {
	case base.DeleteOp:
		return false
	case base.CreateOp:
		return true
	case base.ModifyOp:
		return false
	case base.BindOp:
		return false
	default:
		return false
	}
}

// initializeAdapter is the main function to create database instances
func initializeAdapter(plan catalog.RedisPlan, s *config.Settings, c *catalog.Catalog, logger lager.Logger) (redisAdapter, response.Response) {

	var redisAdapter redisAdapter

	if s.Environment == "test" {
		redisAdapter = &mockRedisAdapter{}
		return redisAdapter, nil
	}

	elasticacheClient := elasticache.New(session.New(), aws.NewConfig().WithRegion(s.Region))
	redisAdapter = &dedicatedRedisAdapter{
		Plan:        plan,
		settings:    *s,
		logger:      logger,
		elasticache: elasticacheClient,
	}
	return redisAdapter, nil
}

func (broker *redisBroker) parseOptionsFromRequest(
	request request.Request,
	plan catalog.RedisPlan,
) (RedisOptions, error) {
	options := RedisOptions{}
	if len(request.RawParameters) > 0 {
		err := json.Unmarshal(request.RawParameters, &options)
		if err != nil {
			return options, err
		}
		err = options.Validate(plan)
		if err != nil {
			return options, err
		}
	}
	return options, nil
}

func (broker *redisBroker) CreateInstance(c *catalog.Catalog, id string, createRequest request.Request) response.Response {
	newInstance := RedisInstance{}

	plan, planErr := c.RedisService.FetchPlan(createRequest.PlanID)
	if planErr != nil {
		return planErr
	}

	options, err := broker.parseOptionsFromRequest(createRequest, plan)
	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, "Invalid parameters. Error: "+err.Error())
	}

	var count int64
	broker.brokerDB.Where("uuid = ?", id).First(&newInstance).Count(&count)
	if count != 0 {
		return response.NewErrorResponse(http.StatusConflict, "The instance already exists")
	}

	if options.EngineVersion != "" {
		// Check to make sure that the version specified is allowed by the plan.
		if !plan.CheckVersion(options.EngineVersion) {
			return response.NewErrorResponse(
				http.StatusBadRequest,
				options.EngineVersion+" is not a supported major version; major version must be one of: 7.0, 6.2, 6.0, 5.0.6 "+".",
			)
		}
	}

	tags, err := broker.tagManager.GenerateTags(
		brokertags.Create,
		c.RedisService.Name,
		plan.Name,
		brokertags.ResourceGUIDs{
			InstanceGUID:     id,
			SpaceGUID:        createRequest.SpaceGUID,
			OrganizationGUID: createRequest.OrganizationGUID,
		},
		false,
	)
	if err != nil {
		return response.NewErrorResponse(http.StatusInternalServerError, "There was an error generating the tags. Error: "+err.Error())
	}

	err = newInstance.init(
		id,
		createRequest.OrganizationGUID,
		createRequest.SpaceGUID,
		createRequest.ServiceID,
		plan,
		options,
		broker.settings,
		tags,
	)

	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, "There was an error initializing the instance. Error: "+err.Error())
	}

	adapter, adapterErr := initializeAdapter(plan, broker.settings, c, broker.logger)
	if adapterErr != nil {
		return adapterErr
	}
	// Create the redis instance.
	status, err := adapter.createRedis(&newInstance, newInstance.ClearPassword)
	if status == base.InstanceNotCreated {
		desc := "There was an error creating the instance."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		return response.NewErrorResponse(http.StatusBadRequest, desc)
	}

	newInstance.State = status
	broker.brokerDB.NewRecord(newInstance)
	err = broker.brokerDB.Create(&newInstance).Error
	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, err.Error())
	}
	return response.SuccessAcceptedResponse
}

func (broker *redisBroker) ModifyInstance(c *catalog.Catalog, id string, modifyRequest request.Request, baseInstance base.Instance) response.Response {
	existingInstance := RedisInstance{}

	newPlan, planErr := c.RedisService.FetchPlan(modifyRequest.PlanID)
	if planErr != nil {
		return planErr
	}

	options, err := broker.parseOptionsFromRequest(modifyRequest, newPlan)
	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, "Invalid parameters. Error: "+err.Error())
	}

	// Load the existing instance provided.
	var count int64
	broker.brokerDB.Where("uuid = ?", id).First(existingInstance).Count(&count)
	if count == 0 {
		return response.NewErrorResponse(http.StatusNotFound, "The instance does not exist.")
	}

	// Check to make sure that we're not switching plans; this is not
	// not yet supported.
	if newPlan.ID != existingInstance.PlanID {
		return response.NewErrorResponse(
			http.StatusBadRequest,
			"Switching plans is not supported.",
		)
	}

	err = existingInstance.modify(options)
	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, "There was an error initializing the instance. Error: "+err.Error())
	}

	adapter, adapterErr := initializeAdapter(newPlan, broker.settings, c, broker.logger)
	if adapterErr != nil {
		return adapterErr
	}

	// Modify the redis instance.
	status, err := adapter.modifyRedis(&existingInstance)
	if status == base.InstanceNotModified {
		desc := "There was an error modifying the instance."

		if err != nil {
			desc = desc + " Error: " + err.Error()
		}

		return response.NewErrorResponse(http.StatusBadRequest, desc)
	}

	// Update the existing instance in the broker.
	existingInstance.State = status
	existingInstance.PlanID = newPlan.ID
	err = broker.brokerDB.Save(existingInstance).Error

	if err != nil {
		return response.NewErrorResponse(http.StatusBadRequest, err.Error())
	}

	return response.SuccessAcceptedResponse
}

func (broker *redisBroker) LastOperation(c *catalog.Catalog, id string, baseInstance base.Instance, operation string) response.Response {
	existingInstance := RedisInstance{}

	var count int64
	broker.brokerDB.Where("uuid = ?", id).First(&existingInstance).Count(&count)
	if count == 0 {
		return response.NewErrorResponse(http.StatusNotFound, "Instance not found")
	}

	plan, planErr := c.RedisService.FetchPlan(baseInstance.PlanID)
	if planErr != nil {
		return planErr
	}

	adapter, adapterErr := initializeAdapter(plan, broker.settings, c, broker.logger)
	if adapterErr != nil {
		return adapterErr
	}

	var state string

	status, _ := adapter.checkRedisStatus(&existingInstance)
	switch status {
	case base.InstanceInProgress:
		state = "in progress"
	case base.InstanceReady:
		state = "succeeded"
	case base.InstanceNotCreated:
		state = "failed"
	case base.InstanceNotGone:
		state = "failed"
	default:
		state = "in progress"
	}
	return response.NewSuccessLastOperation(state, "The service instance status is "+state)
}

func (broker *redisBroker) BindInstance(c *catalog.Catalog, id string, bindRequest request.Request, baseInstance base.Instance) response.Response {
	existingInstance := RedisInstance{}

	var count int64
	broker.brokerDB.Where("uuid = ?", id).First(&existingInstance).Count(&count)
	if count == 0 {
		return response.NewErrorResponse(http.StatusNotFound, "Instance not found")
	}

	plan, planErr := c.RedisService.FetchPlan(baseInstance.PlanID)
	if planErr != nil {
		return planErr
	}

	password, err := existingInstance.getPassword(broker.settings.EncryptionKey)
	if err != nil {
		return response.NewErrorResponse(http.StatusInternalServerError, "Unable to get instance password.")
	}

	// Get the correct database logic depending on the type of plan. (shared vs dedicated)
	adapter, adapterErr := initializeAdapter(plan, broker.settings, c, broker.logger)
	if adapterErr != nil {
		return adapterErr
	}

	var credentials map[string]string
	// Bind the database instance to the application.
	originalInstanceState := existingInstance.State
	if credentials, err = adapter.bindRedisToApp(&existingInstance, password); err != nil {
		desc := "There was an error binding the database instance to the application."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		return response.NewErrorResponse(http.StatusBadRequest, desc)
	}

	// If the state of the instance has changed, update it.
	if existingInstance.State != originalInstanceState {
		broker.brokerDB.Save(&existingInstance)
	}

	return response.NewSuccessBindResponse(credentials)
}

func (broker *redisBroker) DeleteInstance(c *catalog.Catalog, id string, baseInstance base.Instance) response.Response {
	existingInstance := RedisInstance{}
	var count int64
	broker.brokerDB.Where("uuid = ?", id).First(&existingInstance).Count(&count)
	if count == 0 {
		return response.NewErrorResponse(http.StatusNotFound, "Instance not found")
	}

	plan, planErr := c.RedisService.FetchPlan(baseInstance.PlanID)
	if planErr != nil {
		return planErr
	}

	adapter, adapterErr := initializeAdapter(plan, broker.settings, c, broker.logger)
	if adapterErr != nil {
		return adapterErr
	}
	// Delete the database instance.
	if status, err := adapter.deleteRedis(&existingInstance); status == base.InstanceNotGone {
		desc := "There was an error deleting the instance."
		if err != nil {
			desc = desc + " Error: " + err.Error()
		}
		return response.NewErrorResponse(http.StatusBadRequest, desc)
	}
	broker.brokerDB.Unscoped().Delete(&existingInstance)
	return response.SuccessDeleteResponse
}
