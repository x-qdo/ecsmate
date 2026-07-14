package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/x-qdo/ecsmate/internal/config"
)

type templateMockSSMResolver struct{}

func (templateMockSSMResolver) GetParameter(ctx context.Context, name string) (string, error) {
	return "", nil
}

func (templateMockSSMResolver) GetParameters(ctx context.Context, names []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func TestTemplateSSMResolverDisabledByDefault(t *testing.T) {
	oldResolveSSM := templateResolveSSM
	oldNewClient := newTemplateSSMClient
	t.Cleanup(func() {
		templateResolveSSM = oldResolveSSM
		newTemplateSSMClient = oldNewClient
	})

	templateResolveSSM = false
	newTemplateSSMClient = func(ctx context.Context, region string) (config.SSMResolver, error) {
		t.Fatal("template should not initialize SSM client unless --resolve-ssm is set")
		return nil, nil
	}

	resolver, err := templateSSMResolver(context.Background(), &GlobalOptions{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("templateSSMResolver returned error: %v", err)
	}
	if resolver != nil {
		t.Fatalf("expected no resolver by default, got %#v", resolver)
	}
}

func TestTemplateSSMResolverExplicitOptIn(t *testing.T) {
	oldResolveSSM := templateResolveSSM
	oldNewClient := newTemplateSSMClient
	t.Cleanup(func() {
		templateResolveSSM = oldResolveSSM
		newTemplateSSMClient = oldNewClient
	})

	called := false
	templateResolveSSM = true
	newTemplateSSMClient = func(ctx context.Context, region string) (config.SSMResolver, error) {
		called = true
		if region != "us-west-2" {
			t.Fatalf("expected region us-west-2, got %q", region)
		}
		return templateMockSSMResolver{}, nil
	}

	resolver, err := templateSSMResolver(context.Background(), &GlobalOptions{Region: "us-west-2"})
	if err != nil {
		t.Fatalf("templateSSMResolver returned error: %v", err)
	}
	if !called {
		t.Fatal("expected SSM client initialization when --resolve-ssm is set")
	}
	if resolver == nil {
		t.Fatal("expected resolver when --resolve-ssm is set")
	}
}

func TestTemplateSSMResolverNoSSMOverridesOptIn(t *testing.T) {
	oldResolveSSM := templateResolveSSM
	oldNewClient := newTemplateSSMClient
	t.Cleanup(func() {
		templateResolveSSM = oldResolveSSM
		newTemplateSSMClient = oldNewClient
	})

	templateResolveSSM = true
	newTemplateSSMClient = func(ctx context.Context, region string) (config.SSMResolver, error) {
		return nil, errors.New("client should not be initialized when --no-ssm is set")
	}

	resolver, err := templateSSMResolver(context.Background(), &GlobalOptions{NoSSM: true})
	if err != nil {
		t.Fatalf("templateSSMResolver returned error: %v", err)
	}
	if resolver != nil {
		t.Fatalf("expected no resolver with --no-ssm, got %#v", resolver)
	}
}
