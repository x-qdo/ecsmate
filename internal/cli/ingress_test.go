package cli

import (
	"github.com/x-qdo/ecsmate/internal/config"
	"testing"
)

func TestValidateManifestContent_IngressOwnership(t *testing.T) {
	m := &config.Manifest{Name: "app", Ingress: &config.Ingress{Rules: []config.IngressRule{{Priority: 100, Host: "example.com"}}}}
	if len(validateManifestContent(m)) == 0 {
		t.Fatal("untagged ingress must fail offline validation")
	}
	m.Tags = map[string]string{"Environment": "stage", "Application": "app"}
	if errs := validateManifestContent(m); len(errs) > 0 {
		t.Fatalf("valid ownership: %v", errs)
	}
	m.Ingress.Rules = append(m.Ingress.Rules, m.Ingress.Rules[0])
	if len(validateManifestContent(m)) == 0 {
		t.Fatal("duplicate priorities must fail offline validation")
	}
}
