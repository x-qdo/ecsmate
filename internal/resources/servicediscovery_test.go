package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"

	"github.com/x-qdo/ecsmate/internal/config"
)

func TestExtractServiceDiscoverySpecs(t *testing.T) {
	manifest := &config.Manifest{
		Services: map[string]config.Service{
			"web": {
				ServiceRegistries: []config.ServiceRegistry{
					{
						ServiceDiscovery: &config.ServiceDiscoveryConfig{
							NamespaceArn:  "arn:aws:servicediscovery:us-east-1:123:namespace/ns-abc",
							DNSRecordType: "A",
							DNSTTL:        60,
							RoutingPolicy: "MULTIVALUE",
							Tags: map[string]string{
								"Env": "dev",
							},
						},
						ContainerName: "app",
						ContainerPort: 80,
					},
				},
			},
		},
	}

	specs := ExtractServiceDiscoverySpecs(manifest, manifest.Name)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}

	spec, ok := specs["web-sd-0"]
	if !ok {
		t.Fatal("expected spec key web-sd-0")
	}
	if spec.Name != "web" {
		t.Errorf("expected name web, got %q", spec.Name)
	}
	if spec.NamespaceID != "ns-abc" {
		t.Errorf("expected namespace ID ns-abc, got %q", spec.NamespaceID)
	}
	if spec.Tags["Env"] != "dev" {
		t.Errorf("expected tag Env=dev, got %q", spec.Tags["Env"])
	}
}

func TestServiceDiscoveryResource_DetermineAction(t *testing.T) {
	cases := []struct {
		name    string
		desired *ServiceDiscoverySpec
		current *types.Service
		want    ServiceDiscoveryAction
	}{
		{
			name:    "delete when desired nil",
			desired: nil,
			current: &types.Service{},
			want:    ServiceDiscoveryActionDelete,
		},
		{
			name: "create when current nil",
			desired: &ServiceDiscoverySpec{
				Name: "web",
			},
			current: nil,
			want:    ServiceDiscoveryActionCreate,
		},
		{
			name: "update when ttl changed",
			desired: &ServiceDiscoverySpec{
				Name:   "web",
				DNSTTL: 30,
			},
			current: buildServiceDiscoveryCurrent(60),
			want:    ServiceDiscoveryActionUpdate,
		},
		{
			name: "noop when ttl matches",
			desired: &ServiceDiscoverySpec{
				Name:   "web",
				DNSTTL: 60,
			},
			current: buildServiceDiscoveryCurrent(60),
			want:    ServiceDiscoveryActionNoop,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource := &ServiceDiscoveryResource{
				Name:    "web",
				Desired: tc.desired,
				Current: tc.current,
			}
			resource.determineAction()
			if resource.Action != tc.want {
				t.Fatalf("expected action %s, got %s", tc.want, resource.Action)
			}
		})
	}
}

func TestServiceDiscoveryResource_ConfigChanged(t *testing.T) {
	resource := &ServiceDiscoveryResource{
		Desired: &ServiceDiscoverySpec{
			Name:   "web",
			DNSTTL: 120,
		},
		Current: buildServiceDiscoveryCurrent(60),
	}

	if !resource.configChanged() {
		t.Fatal("expected configChanged to be true")
	}
}

func buildServiceDiscoveryCurrent(ttl int64) *types.Service {
	return &types.Service{
		DnsConfig: &types.DnsConfig{
			DnsRecords: []types.DnsRecord{
				{
					Type: types.RecordTypeA,
					TTL:  aws.Int64(ttl),
				},
			},
		},
	}
}
