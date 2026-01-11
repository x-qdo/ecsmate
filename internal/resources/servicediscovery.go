package resources

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type ServiceDiscoveryAction string

const (
	ServiceDiscoveryActionCreate ServiceDiscoveryAction = "CREATE"
	ServiceDiscoveryActionUpdate ServiceDiscoveryAction = "UPDATE"
	ServiceDiscoveryActionDelete ServiceDiscoveryAction = "DELETE"
	ServiceDiscoveryActionNoop   ServiceDiscoveryAction = "NOOP"
)

type ServiceDiscoverySpec struct {
	Name          string
	NamespaceArn  string
	NamespaceID   string
	DNSRecordType string
	DNSTTL        int
	RoutingPolicy string
	Tags          map[string]string

	ECSServiceName string
	ContainerName  string
	ContainerPort  int
	Port           int
}

type ServiceDiscoveryResource struct {
	Name    string
	Desired *ServiceDiscoverySpec
	Current *types.Service
	Action  ServiceDiscoveryAction

	Arn string
	ID  string
}

type ServiceDiscoveryManager struct {
	client *awsclient.ServiceDiscoveryClient
}

func NewServiceDiscoveryManager(client *awsclient.ServiceDiscoveryClient) *ServiceDiscoveryManager {
	return &ServiceDiscoveryManager{client: client}
}

func ExtractServiceDiscoverySpecs(manifest *config.Manifest, manifestName string) map[string]*ServiceDiscoverySpec {
	specs := make(map[string]*ServiceDiscoverySpec)

	for svcName, svc := range manifest.Services {
		for i, reg := range svc.ServiceRegistries {
			if reg.ServiceDiscovery == nil {
				continue
			}

			sd := reg.ServiceDiscovery
			sdName := sd.Name
			if sdName == "" {
				sdName = svcName
			}

			spec := &ServiceDiscoverySpec{
				Name:           sdName,
				NamespaceArn:   sd.NamespaceArn,
				NamespaceID:    sd.NamespaceID,
				DNSRecordType:  sd.DNSRecordType,
				DNSTTL:         sd.DNSTTL,
				RoutingPolicy:  sd.RoutingPolicy,
				Tags:           sd.Tags,
				ECSServiceName: svcName,
				ContainerName:  reg.ContainerName,
				ContainerPort:  reg.ContainerPort,
				Port:           reg.Port,
			}
			if spec.NamespaceID == "" {
				spec.NamespaceID = awsclient.GetNamespaceIDFromArn(sd.NamespaceArn)
			}

			key := fmt.Sprintf("%s-sd-%d", svcName, i)
			specs[key] = spec
		}
	}

	return specs
}

func (m *ServiceDiscoveryManager) BuildResource(ctx context.Context, key string, spec *ServiceDiscoverySpec) (*ServiceDiscoveryResource, error) {
	resource := &ServiceDiscoveryResource{
		Name:    spec.Name,
		Desired: spec,
	}

	if err := m.discoverService(ctx, resource); err != nil {
		log.Debug("failed to discover service discovery service", "name", spec.Name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *ServiceDiscoveryManager) discoverService(ctx context.Context, resource *ServiceDiscoveryResource) error {
	if resource.Desired == nil || resource.Desired.NamespaceID == "" {
		return nil
	}

	services, err := m.client.ListServicesByNamespace(ctx, resource.Desired.NamespaceID)
	if err != nil {
		return err
	}

	for _, svc := range services {
		if aws.ToString(svc.Name) == resource.Name {
			fullSvc, err := m.client.GetService(ctx, aws.ToString(svc.Id))
			if err != nil {
				return err
			}
			resource.Current = fullSvc
			resource.ID = aws.ToString(svc.Id)
			resource.Arn = aws.ToString(svc.Arn)
			return nil
		}
	}

	return nil
}

func (resource *ServiceDiscoveryResource) determineAction() {
	if resource.Desired == nil {
		if resource.Current != nil {
			resource.Action = ServiceDiscoveryActionDelete
		} else {
			resource.Action = ServiceDiscoveryActionNoop
		}
		return
	}

	if resource.Current == nil {
		resource.Action = ServiceDiscoveryActionCreate
		return
	}

	if resource.configChanged() {
		resource.Action = ServiceDiscoveryActionUpdate
		return
	}

	resource.Action = ServiceDiscoveryActionNoop
}

func (resource *ServiceDiscoveryResource) configChanged() bool {
	if resource.Current == nil || resource.Desired == nil {
		return false
	}

	if resource.Current.DnsConfig != nil && len(resource.Current.DnsConfig.DnsRecords) > 0 {
		currentTTL := aws.ToInt64(resource.Current.DnsConfig.DnsRecords[0].TTL)
		if currentTTL != int64(resource.Desired.DNSTTL) {
			return true
		}
	}

	return false
}

func (m *ServiceDiscoveryManager) Create(ctx context.Context, resource *ServiceDiscoveryResource) error {
	log.Info("creating service discovery service", "name", resource.Name)

	input := &awsclient.CreateServiceInput{
		NamespaceID:   resource.Desired.NamespaceID,
		Name:          resource.Name,
		DNSRecordType: resource.Desired.DNSRecordType,
		DNSTTL:        int64(resource.Desired.DNSTTL),
		RoutingPolicy: resource.Desired.RoutingPolicy,
		Tags:          resource.Desired.Tags,
	}

	svc, err := m.client.CreateService(ctx, input)
	if err != nil {
		return err
	}

	resource.ID = aws.ToString(svc.Id)
	resource.Arn = aws.ToString(svc.Arn)
	return nil
}

func (m *ServiceDiscoveryManager) Update(ctx context.Context, resource *ServiceDiscoveryResource) error {
	log.Info("updating service discovery service", "name", resource.Name)

	dnsConfig := &types.DnsConfig{
		DnsRecords: []types.DnsRecord{
			{
				Type: types.RecordType(resource.Desired.DNSRecordType),
				TTL:  aws.Int64(int64(resource.Desired.DNSTTL)),
			},
		},
	}

	return m.client.UpdateService(ctx, resource.ID, dnsConfig)
}

func (m *ServiceDiscoveryManager) Delete(ctx context.Context, resource *ServiceDiscoveryResource) error {
	log.Info("deleting service discovery service", "name", resource.Name)
	return m.client.DeleteService(ctx, resource.ID)
}

func (m *ServiceDiscoveryManager) Apply(ctx context.Context, resource *ServiceDiscoveryResource) error {
	switch resource.Action {
	case ServiceDiscoveryActionCreate:
		return m.Create(ctx, resource)
	case ServiceDiscoveryActionUpdate:
		return m.Update(ctx, resource)
	case ServiceDiscoveryActionDelete:
		return m.Delete(ctx, resource)
	case ServiceDiscoveryActionNoop:
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}
