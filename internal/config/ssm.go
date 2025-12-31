package config

import (
	"context"
	"reflect"
	"regexp"
	"strings"

	"github.com/qdo/ecsmate/internal/log"
)

var ssmRefPattern = regexp.MustCompile(`\{\{ssm:([^}]+)\}\}`)

// SSMResolver resolves SSM parameter references in strings
type SSMResolver interface {
	GetParameter(ctx context.Context, name string) (string, error)
	GetParameters(ctx context.Context, names []string) (map[string]string, error)
}

// ResolveSSMReferences resolves all SSM references in a manifest
func ResolveSSMReferences(ctx context.Context, manifest *Manifest, resolver SSMResolver) error {
	if resolver == nil {
		return nil
	}

	// Collect all SSM references
	refs := collectSSMReferences(manifest)
	if len(refs) == 0 {
		log.Debug("no SSM references found in manifest")
		return nil
	}

	log.Info("resolving SSM parameters", "count", len(refs))

	// Fetch all parameters in batch
	values, err := resolver.GetParameters(ctx, refs)
	if err != nil {
		return err
	}

	// Replace references with values
	replaceSSMReferences(manifest, values)

	return nil
}

// collectSSMReferences finds all SSM references in a manifest
func collectSSMReferences(manifest *Manifest) []string {
	refs := make(map[string]struct{})

	for _, td := range manifest.TaskDefinitions {
		collectFromTaskDef(&td, refs)
	}

	for _, svc := range manifest.Services {
		collectFromService(&svc, refs)
	}

	for _, st := range manifest.ScheduledTasks {
		collectFromScheduledTask(&st, refs)
	}

	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	return result
}

func collectFromTaskDef(td *TaskDefinition, refs map[string]struct{}) {
	collectFromString(td.ExecutionRoleArn, refs)
	collectFromString(td.TaskRoleArn, refs)
	collectFromString(td.BaseArn, refs)
	collectFromString(td.Arn, refs)

	for i := range td.ContainerDefinitions {
		cd := &td.ContainerDefinitions[i]
		collectFromString(cd.Image, refs)
		for _, env := range cd.Environment {
			collectFromString(env.Value, refs)
		}
		for _, secret := range cd.Secrets {
			collectFromString(secret.ValueFrom, refs)
		}
		if cd.LogConfiguration != nil {
			for _, v := range cd.LogConfiguration.Options {
				collectFromString(v, refs)
			}
		}
	}

	if td.Overrides != nil {
		collectFromString(td.Overrides.ExecutionRoleArn, refs)
		collectFromString(td.Overrides.TaskRoleArn, refs)
		for i := range td.Overrides.ContainerDefinitions {
			co := &td.Overrides.ContainerDefinitions[i]
			collectFromString(co.Image, refs)
			for _, env := range co.Environment {
				collectFromString(env.Value, refs)
			}
			for _, secret := range co.Secrets {
				collectFromString(secret.ValueFrom, refs)
			}
		}
	}
}

func collectFromService(svc *Service, refs map[string]struct{}) {
	collectFromString(svc.Cluster, refs)
	if svc.NetworkConfiguration != nil {
		for _, s := range svc.NetworkConfiguration.Subnets {
			collectFromString(s, refs)
		}
		for _, s := range svc.NetworkConfiguration.SecurityGroups {
			collectFromString(s, refs)
		}
	}
	for _, lb := range svc.LoadBalancers {
		collectFromString(lb.TargetGroupArn, refs)
	}
}

func collectFromScheduledTask(st *ScheduledTask, refs map[string]struct{}) {
	collectFromString(st.Cluster, refs)
	if st.NetworkConfiguration != nil {
		for _, s := range st.NetworkConfiguration.Subnets {
			collectFromString(s, refs)
		}
		for _, s := range st.NetworkConfiguration.SecurityGroups {
			collectFromString(s, refs)
		}
	}
	if st.Overrides != nil {
		collectFromString(st.Overrides.TaskRoleArn, refs)
		collectFromString(st.Overrides.ExecutionRoleArn, refs)
	}
}

func collectFromString(s string, refs map[string]struct{}) {
	matches := ssmRefPattern.FindAllStringSubmatch(s, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			refs[strings.TrimSpace(match[1])] = struct{}{}
		}
	}
}

// replaceSSMReferences replaces SSM reference placeholders with actual values
func replaceSSMReferences(manifest *Manifest, values map[string]string) {
	for name, td := range manifest.TaskDefinitions {
		replaceInTaskDef(&td, values)
		manifest.TaskDefinitions[name] = td
	}

	for name, svc := range manifest.Services {
		replaceInService(&svc, values)
		manifest.Services[name] = svc
	}

	for name, st := range manifest.ScheduledTasks {
		replaceInScheduledTask(&st, values)
		manifest.ScheduledTasks[name] = st
	}
}

func replaceInTaskDef(td *TaskDefinition, values map[string]string) {
	td.ExecutionRoleArn = replaceInString(td.ExecutionRoleArn, values)
	td.TaskRoleArn = replaceInString(td.TaskRoleArn, values)
	td.BaseArn = replaceInString(td.BaseArn, values)
	td.Arn = replaceInString(td.Arn, values)

	for i := range td.ContainerDefinitions {
		cd := &td.ContainerDefinitions[i]
		cd.Image = replaceInString(cd.Image, values)
		for j := range cd.Environment {
			cd.Environment[j].Value = replaceInString(cd.Environment[j].Value, values)
		}
		for j := range cd.Secrets {
			cd.Secrets[j].ValueFrom = replaceInString(cd.Secrets[j].ValueFrom, values)
		}
		if cd.LogConfiguration != nil {
			for k, v := range cd.LogConfiguration.Options {
				cd.LogConfiguration.Options[k] = replaceInString(v, values)
			}
		}
	}

	if td.Overrides != nil {
		td.Overrides.ExecutionRoleArn = replaceInString(td.Overrides.ExecutionRoleArn, values)
		td.Overrides.TaskRoleArn = replaceInString(td.Overrides.TaskRoleArn, values)
		for i := range td.Overrides.ContainerDefinitions {
			co := &td.Overrides.ContainerDefinitions[i]
			co.Image = replaceInString(co.Image, values)
			for j := range co.Environment {
				co.Environment[j].Value = replaceInString(co.Environment[j].Value, values)
			}
			for j := range co.Secrets {
				co.Secrets[j].ValueFrom = replaceInString(co.Secrets[j].ValueFrom, values)
			}
		}
	}
}

func replaceInService(svc *Service, values map[string]string) {
	svc.Cluster = replaceInString(svc.Cluster, values)
	if svc.NetworkConfiguration != nil {
		// Replace SSM refs and split comma-separated values (for StringList params)
		svc.NetworkConfiguration.Subnets = replaceAndSplit(svc.NetworkConfiguration.Subnets, values)
		svc.NetworkConfiguration.SecurityGroups = replaceAndSplit(svc.NetworkConfiguration.SecurityGroups, values)
	}
	for i := range svc.LoadBalancers {
		svc.LoadBalancers[i].TargetGroupArn = replaceInString(svc.LoadBalancers[i].TargetGroupArn, values)
	}
	for i := range svc.ServiceRegistries {
		svc.ServiceRegistries[i].RegistryArn = replaceInString(svc.ServiceRegistries[i].RegistryArn, values)
	}
}

// replaceAndSplit replaces SSM refs and splits comma-separated values
func replaceAndSplit(slice []string, values map[string]string) []string {
	var result []string
	for _, s := range slice {
		resolved := replaceInString(s, values)
		// Split comma-separated values (handles SSM StringList params)
		parts := strings.Split(resolved, ",")
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

func replaceInScheduledTask(st *ScheduledTask, values map[string]string) {
	st.Cluster = replaceInString(st.Cluster, values)
	if st.NetworkConfiguration != nil {
		st.NetworkConfiguration.Subnets = replaceAndSplit(st.NetworkConfiguration.Subnets, values)
		st.NetworkConfiguration.SecurityGroups = replaceAndSplit(st.NetworkConfiguration.SecurityGroups, values)
	}
	if st.Overrides != nil {
		st.Overrides.TaskRoleArn = replaceInString(st.Overrides.TaskRoleArn, values)
		st.Overrides.ExecutionRoleArn = replaceInString(st.Overrides.ExecutionRoleArn, values)
	}
}

func replaceInString(s string, values map[string]string) string {
	return ssmRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		matches := ssmRefPattern.FindStringSubmatch(match)
		if len(matches) >= 2 {
			paramName := strings.TrimSpace(matches[1])
			if val, ok := values[paramName]; ok {
				return val
			}
		}
		return match
	})
}

// HasSSMReferences checks if a string contains SSM references
func HasSSMReferences(s string) bool {
	return ssmRefPattern.MatchString(s)
}

// ExtractSSMReferences extracts all SSM parameter names from a string
func ExtractSSMReferences(s string) []string {
	matches := ssmRefPattern.FindAllStringSubmatch(s, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) >= 2 {
			result = append(result, strings.TrimSpace(match[1]))
		}
	}
	return result
}

// ResolveSSMInStruct resolves SSM references in all string fields of a struct using reflection
func ResolveSSMInStruct(ctx context.Context, v interface{}, resolver SSMResolver) error {
	if resolver == nil {
		return nil
	}

	refs := collectRefsFromValue(reflect.ValueOf(v))
	if len(refs) == 0 {
		return nil
	}

	values, err := resolver.GetParameters(ctx, refs)
	if err != nil {
		return err
	}

	replaceRefsInValue(reflect.ValueOf(v), values)
	return nil
}

func collectRefsFromValue(v reflect.Value) []string {
	refs := make(map[string]struct{})
	collectRefsRecursive(v, refs)

	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	return result
}

func collectRefsRecursive(v reflect.Value, refs map[string]struct{}) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			collectRefsRecursive(v.Elem(), refs)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			collectRefsRecursive(v.Field(i), refs)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			collectRefsRecursive(v.Index(i), refs)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			collectRefsRecursive(v.MapIndex(key), refs)
		}
	case reflect.String:
		collectFromString(v.String(), refs)
	}
}

func replaceRefsInValue(v reflect.Value, values map[string]string) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			replaceRefsInValue(v.Elem(), values)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				replaceRefsInValue(field, values)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			replaceRefsInValue(v.Index(i), values)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			mapVal := v.MapIndex(key)
			if mapVal.Kind() == reflect.String {
				newVal := replaceInString(mapVal.String(), values)
				v.SetMapIndex(key, reflect.ValueOf(newVal))
			}
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(replaceInString(v.String(), values))
		}
	}
}
