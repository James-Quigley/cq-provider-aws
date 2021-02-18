package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/cloudquery/cq-provider-aws/cloudwatch"
	"github.com/cloudquery/cq-provider-aws/cloudwatchlogs"
	"github.com/cloudquery/cq-provider-aws/directconnect"
	"github.com/cloudquery/cq-provider-aws/ec2"
	"github.com/cloudquery/cq-provider-aws/ecr"
	"github.com/cloudquery/cq-provider-aws/ecs"
	"github.com/cloudquery/cq-provider-aws/efs"
	"github.com/cloudquery/cq-provider-aws/elasticbeanstalk"
	"github.com/cloudquery/cq-provider-aws/elbv2"
	"github.com/cloudquery/cq-provider-aws/emr"
	"github.com/cloudquery/cq-provider-aws/fsx"
	"github.com/cloudquery/cq-provider-aws/iam"
	"github.com/cloudquery/cq-provider-aws/kms"
	"github.com/cloudquery/cq-provider-aws/organizations"
	"github.com/cloudquery/cq-provider-aws/rds"
	"github.com/cloudquery/cq-provider-aws/redshift"
	"github.com/cloudquery/cq-provider-aws/s3"
	"github.com/cloudquery/cq-provider-aws/sns"

	"github.com/cloudquery/cloudquery/cqlog"
	"github.com/cloudquery/cloudquery/sdk"
	"github.com/cloudquery/cq-provider-aws/autoscaling"
	"github.com/cloudquery/cq-provider-aws/cloudtrail"
	"log"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudquery/cloudquery/database"
	"github.com/cloudquery/cq-provider-aws/resource"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type Provider struct {
	cfg				aws.Config
	region          string
	db              *database.Database
	config          Config
	accountID       string
	resourceClients map[string]resource.ClientInterface
	log             *zap.Logger
	//logLevel        aws.LogLevelType
}

type Account struct {
	ID      string
	RoleARN string
}

type Config struct {
	Regions    []string
	Accounts   []Account
	LogLevel   *string
	MaxRetries *int
	Resources  []struct {
		Name  string
		Other map[string]interface{} `yaml:",inline"`
	}
}

var globalCollectedResources = map[string]bool{}

type ServiceNewFunction func(awsConfig aws.Config, db *database.Database, log *zap.Logger, accountID string, region string) resource.ClientInterface

var globalServices = map[string]ServiceNewFunction{
	"iam":           iam.NewClient,
	"s3":            s3.NewClient,
	"organizations": organizations.NewClient,
}

var regionalServices = map[string]ServiceNewFunction{
	"autoscaling":      autoscaling.NewClient,
	"cloudtrail":       cloudtrail.NewClient,
	"cloudwatchlogs":   cloudwatchlogs.NewClient,
	"cloudwatch":       cloudwatch.NewClient,
	"directconnect":    directconnect.NewClient,
	"ec2":              ec2.NewClient,
	"ecr":              ecr.NewClient,
	"ecs":              ecs.NewClient,
	"efs":              efs.NewClient,
	"elasticbeanstalk": elasticbeanstalk.NewClient,
	"elbv2":            elbv2.NewClient,
	"emr":              emr.NewClient,
	"fsx":              fsx.NewClient,
	"kms":              kms.NewClient,
	"rds":              rds.NewClient,
	"redshift":         redshift.NewClient,
	"sns":              sns.NewClient,
}

var tablesArr = [][]interface{}{
	autoscaling.LaunchConfigurationTables,
	cloudtrail.TrailTables,
	cloudwatchlogs.MetricFilterTables,
	cloudwatch.MetricAlarmTables,
	directconnect.GatewayTables,
	ec2.ByoipCidrTables,
	ec2.CustomerGatewayTables,
	ec2.FlowLogsTables,
	ec2.ImageTables,
	ec2.InstanceTables,
	ec2.InternetGatewayTables,
	ec2.NatGatewayTables,
	ec2.NetworkAclTables,
	ec2.RouteTableTables,
	ec2.SecurityGroupTables,
	ec2.SubnetTables,
	ec2.VPCPeeringConnectionTables,
	ec2.VPCTables,
	ecr.ImageTables,
	ecs.ClusterTables,
	efs.FileSystemTables,
	elasticbeanstalk.EnvironmentTables,
	elbv2.LoadBalancerTables,
	elbv2.TargetGroupTables,
	emr.ClusterTables,
	fsx.BackupTables,
	iam.GroupTables,
	iam.PasswordPolicyTables,
	iam.PolicyTables,
	iam.RoleTables,
	iam.UserTables,
	iam.VirtualMFADeviceTables,
	kms.KeyTables,
	organizations.AccountTables,
	rds.ClusterTables,
	rds.CertificateTables,
	rds.DBSubnetGroupTables,
	redshift.ClusterTables,
	redshift.ClusterSubnetGroupTables,
	s3.BucketTables,
	sns.SubscriptionTables,
	sns.TopicTables,
}

func (p *Provider) Init(driver string, dsn string, verbose bool) error {
	var err error
	p.db, err = database.Open(driver, dsn)
	if err != nil {
		return err
	}

	zapLogger, err := cqlog.NewLogger(verbose)
	p.log = zapLogger
	p.resourceClients = map[string]resource.ClientInterface{}
	p.log.Info("Creating tables if needed")
	for _, tables := range tablesArr {
		err := p.db.AutoMigrate(tables...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) GenConfig() (string, error) {
	return configYaml, nil
}

//func (p *Provider) parseLogLevel() {
//	if p.config.LogLevel == nil {
//		return
//	}
//	switch *p.config.LogLevel {
//	case "debug", "debug_with_signing":
//		p.logLevel = aws.LogDebug
//	case "debug_with_http_body":
//		p.logLevel = aws.LogDebugWithSigning
//	case "debug_with_request_retries":
//		p.logLevel = aws.LogDebugWithRequestRetries
//	case "debug_with_request_error":
//		p.logLevel = aws.LogDebugWithRequestErrors
//	case "debug_with_event_stream_body":
//		p.logLevel = aws.LogDebugWithEventStreamBody
//	default:
//		log.Fatalf("unknown log_level %s", *p.config.LogLevel)
//	}
//}

var allRegions = []string {
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"af-south-1",
	"ap-east-1",
	"ap-south-1",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-southeast-1",
	"ap-southeast-2",
	"ca-central-1",
	"cn-north-1",
	"cn-northwest-1",
	"eu-central-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-south-1",
	"eu-north-1",
	"me-south-1",
	"sa-east-1",
}

func (p *Provider) Fetch(data []byte) error {
	err := yaml.Unmarshal(data, &p.config)
	ctx := context.Background()
	var ae smithy.APIError
	if err != nil {
		return err
	}

	if len(p.config.Resources) == 0 {
		p.log.Info("no resources specified. See available resources: see: https://docs.cloudquery.io/aws/tables-reference")
		return nil
	}
	regions := p.config.Regions
	if len(regions) == 0 {
		regions = allRegions
		p.log.Info(fmt.Sprintf("No regions specified in config.yml. Assuming all %d regions", len(regions)))
	}

	if len(p.config.Accounts) == 0 {
		p.config.Accounts = append(p.config.Accounts, Account{
			ID:      "default",
			RoleARN: "default",
		})
	}

	for _, account := range p.config.Accounts {
		if account.ID != "default" && account.RoleARN != "" {
			// assume role if specified (SDK takes it from default or env var: AWS_PROFILE)
			p.cfg, err = config.LoadDefaultConfig(ctx)
			if err != nil {
				return err
			}
			provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(p.cfg), account.RoleARN)
			_, err = provider.Retrieve(ctx)
			if err != nil {
				return err
			}

		} else if account.ID != "default" {
			p.cfg, err = config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(account.ID))
		} else {
			p.cfg, err = config.LoadDefaultConfig(ctx)
		}
		if err != nil {
			return err
		}
		svc := sts.NewFromConfig(p.cfg)
		output, err := svc.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}, func(o *sts.Options) {
			o.Region = "us-east-1"
		})
		if err != nil {
			return err
		}
		p.accountID = *output.Account


		for _, region := range regions {
			p.region = region

			// Find a better way in AWS SDK V2 to decide if a region is disabled.
			_, err := svc.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}, func(o *sts.Options) {
				o.Region = region
			})
			if err != nil {
				if errors.As(err, &ae) && (ae.ErrorCode() == "InvalidClientTokenId" || ae.ErrorCode() == "OptInRequired") {
					p.log.Info("region disabled. skipping...", zap.String("region", region))
					continue
				}
				return err
			}

			p.initRegionalClients()
			var wg sync.WaitGroup
			for _, resource := range p.config.Resources {
				wg.Add(1)
				go p.collectResource(&wg, resource.Name, resource.Other)
			}
			wg.Wait()
		}
		globalCollectedResources = map[string]bool{}
		p.resourceClients = map[string]resource.ClientInterface{}
	}

	return nil
}

func (p *Provider) initRegionalClients() {
	zapLog := p.log.With(zap.String("account_id", p.accountID), zap.String("region", p.region))
	for serviceName, newFunc := range regionalServices {
		p.resourceClients[serviceName] = newFunc(p.cfg,
			p.db, zapLog, p.accountID, p.region)
	}
}

var lock = sync.RWMutex{}

func (p *Provider) collectResource(wg *sync.WaitGroup, fullResourceName string, config interface{}) {
	defer wg.Done()
	resourcePath := strings.Split(fullResourceName, ".")
	if len(resourcePath) != 2 {
		log.Fatalf("resource %s should be in format {service}.{resource}", fullResourceName)
	}
	service := resourcePath[0]
	resourceName := resourcePath[1]

	if globalServices[service] != nil {
		lock.Lock()
		if globalCollectedResources[fullResourceName] {
			lock.Unlock()
			return
		}
		globalCollectedResources[fullResourceName] = true
		if p.resourceClients[service] == nil {
			zapLog := p.log.With(zap.String("account_id", p.accountID))
			p.resourceClients[service] = globalServices[service](p.cfg,
				p.db, zapLog, p.accountID, p.region)
		}
		lock.Unlock()
	}

	if p.resourceClients[service] == nil {
		log.Fatalf("unsupported service %s for resource %s", service, resourceName)
	}

	err := p.resourceClients[service].CollectResource(resourceName, config)
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			switch ae.ErrorCode() {
			case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation":
				p.log.Info("Skipping resource. Access denied", zap.String("account_id", p.accountID), zap.String("region", p.region), zap.String("resource", fullResourceName), zap.Error(err))
				return
			case "OptInRequired", "SubscriptionRequiredException":
				p.log.Info("Skipping resource. Service disabled", zap.String("account_id", p.accountID), zap.String("region", p.region), zap.String("resource", fullResourceName), zap.Error(err))
				return
			}
		}
		log.Fatal(err)
	}
}

func main() {
	sdk.ServePlugin(&Provider{})
}
