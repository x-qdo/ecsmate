package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"
)

type ServiceDiscoveryClient struct {
	client *servicediscovery.Client
}

const (
	tagKeyManagedBy = "ManagedBy"
	tagValueEcsmate = "ecsmate"
)

func NewServiceDiscoveryClient(ctx context.Context, region string) (*ServiceDiscoveryClient, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &ServiceDiscoveryClient{
		client: servicediscovery.NewFromConfig(cfg),
	}, nil
}

type CreateServiceInput struct {
	NamespaceID   string
	Name          string
	DNSRecordType string
	DNSTTL        int64
	RoutingPolicy string
	Tags          map[string]string
}

func (c *ServiceDiscoveryClient) CreateService(ctx context.Context, input *CreateServiceInput) (*types.Service, error) {
	dnsConfig := &types.DnsConfig{
		RoutingPolicy: types.RoutingPolicy(input.RoutingPolicy),
		DnsRecords: []types.DnsRecord{
			{
				Type: types.RecordType(input.DNSRecordType),
				TTL:  aws.Int64(input.DNSTTL),
			},
		},
	}

	tags := []types.Tag{
		{Key: aws.String(tagKeyManagedBy), Value: aws.String(tagValueEcsmate)},
	}
	for k, v := range input.Tags {
		if k == tagKeyManagedBy {
			continue
		}
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	createInput := &servicediscovery.CreateServiceInput{
		Name:        aws.String(input.Name),
		NamespaceId: aws.String(input.NamespaceID),
		DnsConfig:   dnsConfig,
		Tags:        tags,
	}

	result, err := c.client.CreateService(ctx, createInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create service discovery service: %w", err)
	}

	return result.Service, nil
}

func (c *ServiceDiscoveryClient) GetService(ctx context.Context, serviceID string) (*types.Service, error) {
	input := &servicediscovery.GetServiceInput{
		Id: aws.String(serviceID),
	}

	result, err := c.client.GetService(ctx, input)
	if err != nil {
		return nil, err
	}

	return result.Service, nil
}

func (c *ServiceDiscoveryClient) UpdateService(ctx context.Context, serviceID string, dnsConfig *types.DnsConfig) error {
	input := &servicediscovery.UpdateServiceInput{
		Id: aws.String(serviceID),
		Service: &types.ServiceChange{
			DnsConfig: &types.DnsConfigChange{
				DnsRecords: dnsConfig.DnsRecords,
			},
		},
	}

	_, err := c.client.UpdateService(ctx, input)
	return err
}

func (c *ServiceDiscoveryClient) DeleteService(ctx context.Context, serviceID string) error {
	input := &servicediscovery.DeleteServiceInput{
		Id: aws.String(serviceID),
	}

	_, err := c.client.DeleteService(ctx, input)
	return err
}

func (c *ServiceDiscoveryClient) ListServicesByNamespace(ctx context.Context, namespaceID string) ([]types.ServiceSummary, error) {
	var services []types.ServiceSummary
	paginator := servicediscovery.NewListServicesPaginator(c.client, &servicediscovery.ListServicesInput{
		Filters: []types.ServiceFilter{
			{
				Name:      types.ServiceFilterNameNamespaceId,
				Values:    []string{namespaceID},
				Condition: types.FilterConditionEq,
			},
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		services = append(services, page.Services...)
	}

	return services, nil
}

func (c *ServiceDiscoveryClient) ListTagsForResource(ctx context.Context, arn string) (map[string]string, error) {
	result, err := c.client.ListTagsForResource(ctx, &servicediscovery.ListTagsForResourceInput{
		ResourceARN: aws.String(arn),
	})
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	for _, tag := range result.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}

	return tags, nil
}

func GetNamespaceIDFromArn(arn string) string {
	parts := strings.Split(arn, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

func GetServiceIDFromArn(arn string) string {
	parts := strings.Split(arn, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}
