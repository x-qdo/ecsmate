package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/qdo/ecsmate/internal/config"
)

func TestTargetGroupResource_ConfigChanged_HealthCheck(t *testing.T) {
	baseCurrent := &types.TargetGroup{
		Port:                       aws.Int32(80),
		Protocol:                   types.ProtocolEnumHttp,
		HealthCheckPath:            aws.String("/health"),
		HealthCheckProtocol:        types.ProtocolEnumHttp,
		HealthCheckPort:            aws.String("traffic-port"),
		HealthCheckIntervalSeconds: aws.Int32(30),
		HealthCheckTimeoutSeconds:  aws.Int32(5),
		HealthyThresholdCount:      aws.Int32(2),
		UnhealthyThresholdCount:    aws.Int32(2),
		Matcher:                    &types.Matcher{HttpCode: aws.String("200")},
	}

	baseDesired := &TargetGroupSpec{
		Port:     80,
		Protocol: "HTTP",
		HealthCheck: &config.TargetGroupHealthCheck{
			Path:               "/health",
			Protocol:           "HTTP",
			Port:               "traffic-port",
			Interval:           30,
			Timeout:            5,
			HealthyThreshold:   2,
			UnhealthyThreshold: 2,
			Matcher:            "200",
		},
	}

	cases := []struct {
		name    string
		desired *TargetGroupSpec
		changed bool
	}{
		{
			name:    "no changes",
			desired: baseDesired,
			changed: false,
		},
		{
			name: "path changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Path: "/status",
				},
			},
			changed: true,
		},
		{
			name: "protocol changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Protocol: "HTTPS",
				},
			},
			changed: true,
		},
		{
			name: "port changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Port: "8080",
				},
			},
			changed: true,
		},
		{
			name: "interval changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Interval: 31,
				},
			},
			changed: true,
		},
		{
			name: "timeout changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Timeout: 6,
				},
			},
			changed: true,
		},
		{
			name: "healthy threshold changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					HealthyThreshold: 3,
				},
			},
			changed: true,
		},
		{
			name: "unhealthy threshold changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					UnhealthyThreshold: 3,
				},
			},
			changed: true,
		},
		{
			name: "matcher changed",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
				HealthCheck: &config.TargetGroupHealthCheck{
					Matcher: "200-299",
				},
			},
			changed: true,
		},
		{
			name: "no desired health check",
			desired: &TargetGroupSpec{
				Port:     80,
				Protocol: "HTTP",
			},
			changed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource := &TargetGroupResource{
				Current: baseCurrent,
				Desired: tc.desired,
			}
			if got := resource.mutableConfigChanged(); got != tc.changed {
				t.Fatalf("expected changed=%t, got %t", tc.changed, got)
			}
		})
	}
}

func TestTargetGroupResource_DetermineAction_UpdateOnMutableChange(t *testing.T) {
	resource := &TargetGroupResource{
		Desired: &TargetGroupSpec{
			Port:     80,
			Protocol: "HTTP",
			HealthCheck: &config.TargetGroupHealthCheck{
				Timeout: 10,
			},
		},
		Current: &types.TargetGroup{
			Port:                      aws.Int32(80),
			Protocol:                  types.ProtocolEnumHttp,
			HealthCheckTimeoutSeconds: aws.Int32(5),
		},
	}

	resource.determineAction()

	if resource.Action != TargetGroupActionUpdate {
		t.Fatalf("expected action UPDATE, got %s", resource.Action)
	}
}

func TestTargetGroupResource_DetermineAction_RecreateOnImmutableChange(t *testing.T) {
	resource := &TargetGroupResource{
		Desired: &TargetGroupSpec{
			Port:     8080,
			Protocol: "HTTP",
		},
		Current: &types.TargetGroup{
			Port:     aws.Int32(80),
			Protocol: types.ProtocolEnumHttp,
		},
	}

	resource.determineAction()

	if resource.Action != TargetGroupActionRecreate {
		t.Fatalf("expected action RECREATE, got %s", resource.Action)
	}
	if len(resource.RecreateReasons) == 0 {
		t.Fatal("expected recreate reasons to be set")
	}
}
