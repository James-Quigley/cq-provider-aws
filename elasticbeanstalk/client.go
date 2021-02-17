package elasticbeanstalk

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	"github.com/cloudquery/cloudquery/database"
	"github.com/cloudquery/cq-provider-aws/resource"
	"go.uber.org/zap"
)

type Client struct {
	db        *database.Database
	log       *zap.Logger
	accountID string
	region    string
	svc       *elasticbeanstalk.Client
}

func NewClient(awsConfig aws.Config, db *database.Database, log *zap.Logger,
	accountID string, region string) resource.ClientInterface {
	return &Client{
		db:        db,
		log:       log,
		accountID: accountID,
		region:    region,
		svc:       elasticbeanstalk.NewFromConfig(awsConfig),
	}
}

func (c *Client) CollectResource(resource string, config interface{}) error {
	switch resource {
	case "environments":
		return c.environments(config)
	default:
		return fmt.Errorf("unsupported resource autoscaling.%s", resource)
	}
}
