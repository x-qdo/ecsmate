package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/diff"
	"github.com/qdo/ecsmate/internal/resources"
)

type Plan struct {
	State   *resources.DesiredState
	Entries []diff.DiffEntry
	Summary diff.DiffSummary
}

type Planner struct{}

func NewPlanner() *Planner {
	return &Planner{}
}

func (p *Planner) GeneratePlan(state *resources.DesiredState) *Plan {
	plan := &Plan{
		State:   state,
		Entries: make([]diff.DiffEntry, 0),
	}

	// Build resource graph and propagate changes
	graph, err := BuildResourceGraph(state)
	if err == nil {
		changes := graph.PropagateChanges()
		applyPropagatedChanges(state, changes)
	}

	p.planTaskDefs(state, plan)
	p.planTargetGroups(state, plan)
	p.planListenerRules(state, plan)
	p.planServices(state, plan)
	p.planScheduledTasks(state, plan)

	plan.Summary = p.calculateSummary(plan.Entries)

	return plan
}

// applyPropagatedChanges applies propagated changes from the DAG to the resource state.
// For ListenerRules, suppresses propagation only when there are no actual config changes
// AND the propagation isn't due to a recreation (which always requires update for new ARN).
func applyPropagatedChanges(state *resources.DesiredState, changes map[string]*Change) {
	for nodeID, change := range changes {
		if change.Reason == "" {
			continue // Skip direct changes (not propagated)
		}

		parts := strings.SplitN(nodeID, "/", 2)
		if len(parts) != 2 {
			continue
		}
		nodeType, nodeName := parts[0], parts[1]

		switch nodeType {
		case "Service":
			if svc, ok := state.Services[nodeName]; ok && svc.Action == resources.ServiceActionNoop {
				// Services always need update when dependency changes (new task def revision, etc.)
				svc.Action = serviceActionFromString(change.Action)
				svc.PropagationReason = change.Reason
			}
		case "ListenerRule":
			for _, rule := range state.ListenerRules {
				if fmt.Sprintf("priority-%d", rule.Priority) == nodeName && rule.Action == resources.ListenerRuleActionNoop {
					// Always propagate if:
					// - Change is DELETE (rule must be deleted with its target group)
					// - Change is due to recreation (new ARN will be assigned)
					// Only suppress if there are no config changes AND it's a simple update
					isDelete := change.Action == "DELETE"
					isRecreation := strings.Contains(change.Reason, "recreated")
					if isDelete || isRecreation || rule.HasConfigChanges() {
						rule.Action = listenerRuleActionFromString(change.Action)
						rule.PropagationReason = change.Reason
					}
				}
			}
		case "ScheduledTask":
			if task, ok := state.ScheduledTasks[nodeName]; ok && task.Action == resources.ScheduledTaskActionNoop {
				// ScheduledTasks always need update when task def changes
				task.Action = scheduledTaskActionFromString(change.Action)
				task.PropagationReason = change.Reason
			}
		}
	}
}

func serviceActionFromString(action string) resources.ServiceAction {
	switch action {
	case "CREATE":
		return resources.ServiceActionCreate
	case "UPDATE":
		return resources.ServiceActionUpdate
	case "DELETE":
		return resources.ServiceActionDelete
	case "RECREATE":
		return resources.ServiceActionRecreate
	default:
		return resources.ServiceActionNoop
	}
}

func listenerRuleActionFromString(action string) resources.ListenerRuleAction {
	switch action {
	case "CREATE":
		return resources.ListenerRuleActionCreate
	case "UPDATE":
		return resources.ListenerRuleActionUpdate
	case "DELETE":
		return resources.ListenerRuleActionDelete
	default:
		return resources.ListenerRuleActionNoop
	}
}

func scheduledTaskActionFromString(action string) resources.ScheduledTaskAction {
	switch action {
	case "CREATE":
		return resources.ScheduledTaskActionCreate
	case "UPDATE":
		return resources.ScheduledTaskActionUpdate
	case "DELETE":
		return resources.ScheduledTaskActionDelete
	default:
		return resources.ScheduledTaskActionNoop
	}
}

func (p *Planner) planTaskDefs(state *resources.DesiredState, plan *Plan) {
	for name, td := range state.TaskDefs {
		entry := diff.DiffEntry{
			Name:     name,
			Resource: "TaskDefinition",
		}

		switch td.Action {
		case resources.TaskDefActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildTaskDefView(td)
			plan.Summary.Creates++

		case resources.TaskDefActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Current = buildTaskDefCurrentView(td)
			entry.Desired = buildTaskDefView(td)
			plan.Summary.Updates++

		case resources.TaskDefActionNoop:
			entry.Type = diff.DiffTypeNoop
			plan.Summary.Noops++
		}

		plan.Entries = append(plan.Entries, entry)
	}
}

func (p *Planner) planServices(state *resources.DesiredState, plan *Plan) {
	var ingress *config.Ingress
	if state.Manifest != nil {
		ingress = state.Manifest.Ingress
	}

	for name, svc := range state.Services {
		entry := diff.DiffEntry{
			Name:              name,
			Resource:          "Service",
			PropagationReason: svc.PropagationReason,
		}

		switch svc.Action {
		case resources.ServiceActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildServiceView(svc, ingress, state.TaskDefs, state.TargetGroups, manifestName(state))
			plan.Summary.Creates++

		case resources.ServiceActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Current = buildServiceCurrentView(svc)
			entry.Desired = buildServiceView(svc, ingress, state.TaskDefs, state.TargetGroups, manifestName(state))
			plan.Summary.Updates++

		case resources.ServiceActionRecreate:
			entry.Type = diff.DiffTypeRecreate
			entry.Current = buildServiceCurrentView(svc)
			entry.Desired = buildServiceView(svc, ingress, state.TaskDefs, state.TargetGroups, manifestName(state))
			entry.RecreateReasons = svc.RecreateReasons
			plan.Summary.Recreates++

		case resources.ServiceActionDelete:
			entry.Type = diff.DiffTypeDelete
			entry.Current = buildServiceCurrentView(svc)
			plan.Summary.Deletes++

		case resources.ServiceActionNoop:
			entry.Type = diff.DiffTypeNoop
			plan.Summary.Noops++
		}

		plan.Entries = append(plan.Entries, entry)
	}
}

func (p *Planner) planScheduledTasks(state *resources.DesiredState, plan *Plan) {
	for name, task := range state.ScheduledTasks {
		entry := diff.DiffEntry{
			Name:              name,
			Resource:          "ScheduledTask",
			PropagationReason: task.PropagationReason,
		}

		switch task.Action {
		case resources.ScheduledTaskActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildScheduledTaskView(task)
			plan.Summary.Creates++

		case resources.ScheduledTaskActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Desired = buildScheduledTaskView(task)
			plan.Summary.Updates++

		case resources.ScheduledTaskActionDelete:
			entry.Type = diff.DiffTypeDelete
			plan.Summary.Deletes++

		case resources.ScheduledTaskActionNoop:
			entry.Type = diff.DiffTypeNoop
			plan.Summary.Noops++
		}

		plan.Entries = append(plan.Entries, entry)
	}
}

func (p *Planner) planTargetGroups(state *resources.DesiredState, plan *Plan) {
	for key, tg := range state.TargetGroups {
		entry := diff.DiffEntry{
			Name:     tg.Name,
			Resource: "TargetGroup",
		}

		switch tg.Action {
		case resources.TargetGroupActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildTargetGroupView(tg)
			plan.Summary.Creates++

		case resources.TargetGroupActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Current = buildTargetGroupCurrentView(tg)
			entry.Desired = buildTargetGroupView(tg)
			plan.Summary.Updates++

		case resources.TargetGroupActionRecreate:
			entry.Type = diff.DiffTypeRecreate
			entry.Current = buildTargetGroupCurrentView(tg)
			entry.Desired = buildTargetGroupView(tg)
			entry.RecreateReasons = tg.RecreateReasons
			entry.Details = targetGroupRecreateDetails(state, tg)
			plan.Summary.Recreates++

		case resources.TargetGroupActionDelete:
			entry.Type = diff.DiffTypeDelete
			entry.Current = buildTargetGroupCurrentView(tg)
			plan.Summary.Deletes++

		case resources.TargetGroupActionNoop:
			entry.Type = diff.DiffTypeNoop
			plan.Summary.Noops++
		}

		_ = key
		plan.Entries = append(plan.Entries, entry)
	}
}

func targetGroupRecreateDetails(state *resources.DesiredState, tg *resources.TargetGroupResource) string {
	if state == nil || tg == nil || tg.Arn == "" {
		return ""
	}

	var deps []string
	for _, rule := range state.ListenerRules {
		if rule == nil || rule.TargetGroupArn == "" {
			continue
		}
		if rule.TargetGroupArn == tg.Arn {
			deps = append(deps, fmt.Sprintf("ListenerRule/priority-%d", rule.Priority))
		}
	}

	if len(deps) == 0 {
		return ""
	}

	sort.Strings(deps)

	var b strings.Builder
	b.WriteString("dependent listener rules:\n")
	for _, dep := range deps {
		b.WriteString("  - ")
		b.WriteString(dep)
		b.WriteString("\n")
	}
	b.WriteString("apply order:\n")
	b.WriteString("  - update listener rules to detach from target group\n")
	b.WriteString("  - recreate target group\n")
	b.WriteString("  - update listener rules to attach new target group\n")

	return strings.TrimSuffix(b.String(), "\n")
}

func (p *Planner) planListenerRules(state *resources.DesiredState, plan *Plan) {
	for _, rule := range state.ListenerRules {
		entry := diff.DiffEntry{
			Name:              fmt.Sprintf("priority-%d", rule.Priority),
			Resource:          "ListenerRule",
			PropagationReason: rule.PropagationReason,
		}

		switch rule.Action {
		case resources.ListenerRuleActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildListenerRuleView(rule)
			plan.Summary.Creates++

		case resources.ListenerRuleActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Current = buildListenerRuleCurrentView(rule)
			entry.Desired = buildListenerRuleView(rule)
			plan.Summary.Updates++

		case resources.ListenerRuleActionDelete:
			entry.Type = diff.DiffTypeDelete
			entry.Current = buildListenerRuleCurrentView(rule)
			plan.Summary.Deletes++

		case resources.ListenerRuleActionNoop:
			entry.Type = diff.DiffTypeNoop
			plan.Summary.Noops++
		}

		plan.Entries = append(plan.Entries, entry)
	}
}

func (p *Planner) calculateSummary(entries []diff.DiffEntry) diff.DiffSummary {
	summary := diff.DiffSummary{}
	for _, e := range entries {
		switch e.Type {
		case diff.DiffTypeCreate:
			summary.Creates++
		case diff.DiffTypeUpdate:
			summary.Updates++
		case diff.DiffTypeDelete:
			summary.Deletes++
		case diff.DiffTypeRecreate:
			summary.Recreates++
		case diff.DiffTypeNoop:
			summary.Noops++
		}
	}
	return summary
}

func (plan *Plan) HasChanges() bool {
	return plan.Summary.Creates > 0 || plan.Summary.Updates > 0 || plan.Summary.Deletes > 0 || plan.Summary.Recreates > 0
}

type TaskDefView struct {
	Type                    string             `json:"type"`
	Family                  string             `json:"family,omitempty"`
	CPU                     string             `json:"cpu,omitempty"`
	Memory                  string             `json:"memory,omitempty"`
	NetworkMode             string             `json:"networkMode,omitempty"`
	RequiresCompatibilities []string           `json:"requiresCompatibilities,omitempty"`
	ExecutionRoleArn        string             `json:"executionRoleArn,omitempty"`
	TaskRoleArn             string             `json:"taskRoleArn,omitempty"`
	ContainerDefinitions    []ContainerDefView `json:"containerDefinitions,omitempty"`
	Arn                     string             `json:"arn,omitempty"`
	BaseArn                 string             `json:"baseArn,omitempty"`
}

type ContainerDefView struct {
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	CPU          int               `json:"cpu,omitempty"`
	Memory       int               `json:"memory,omitempty"`
	Essential    bool              `json:"essential"`
	Command      []string          `json:"command,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
	Secrets      map[string]string `json:"secrets,omitempty"`
	PortMappings []PortMappingView `json:"portMappings,omitempty"`
}

type PortMappingView struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

func buildTaskDefView(td *resources.TaskDefResource) TaskDefView {
	view := TaskDefView{
		Type: td.Type,
	}

	if td.Desired != nil {
		view.Family = td.Desired.Family
		view.CPU = td.Desired.CPU
		view.Memory = td.Desired.Memory
		view.NetworkMode = td.Desired.NetworkMode
		view.RequiresCompatibilities = td.Desired.RequiresCompatibilities
		view.ExecutionRoleArn = td.Desired.ExecutionRoleArn
		view.TaskRoleArn = td.Desired.TaskRoleArn

		if td.Type == "remote" {
			view.Arn = td.Desired.Arn
		}
		if td.Type == "merged" {
			view.BaseArn = td.Desired.BaseArn
		}

		for _, cd := range td.Desired.ContainerDefinitions {
			cdView := ContainerDefView{
				Name:      cd.Name,
				Image:     cd.Image,
				CPU:       cd.CPU,
				Memory:    cd.Memory,
				Essential: cd.Essential,
				Command:   cd.Command,
			}

			if len(cd.Environment) > 0 {
				cdView.Environment = make(map[string]string)
				for _, env := range cd.Environment {
					cdView.Environment[env.Name] = env.Value
				}
			}

			if len(cd.Secrets) > 0 {
				cdView.Secrets = make(map[string]string)
				for _, secret := range cd.Secrets {
					cdView.Secrets[secret.Name] = secret.ValueFrom
				}
			}

			for _, pm := range cd.PortMappings {
				cdView.PortMappings = append(cdView.PortMappings, PortMappingView{
					ContainerPort: pm.ContainerPort,
					HostPort:      pm.HostPort,
					Protocol:      pm.Protocol,
				})
			}

			view.ContainerDefinitions = append(view.ContainerDefinitions, cdView)
		}
	}

	return view
}

func buildTaskDefCurrentView(td *resources.TaskDefResource) TaskDefView {
	view := TaskDefView{
		Type: td.Type,
	}

	if td.Current != nil {
		if td.Current.Family != nil {
			view.Family = *td.Current.Family
		}
		if td.Current.Cpu != nil {
			view.CPU = *td.Current.Cpu
		}
		if td.Current.Memory != nil {
			view.Memory = *td.Current.Memory
		}
		view.NetworkMode = string(td.Current.NetworkMode)
		if td.Current.ExecutionRoleArn != nil {
			view.ExecutionRoleArn = *td.Current.ExecutionRoleArn
		}
		if td.Current.TaskRoleArn != nil {
			view.TaskRoleArn = *td.Current.TaskRoleArn
		}

		for _, compat := range td.Current.RequiresCompatibilities {
			view.RequiresCompatibilities = append(view.RequiresCompatibilities, string(compat))
		}

		for _, cd := range td.Current.ContainerDefinitions {
			cdView := ContainerDefView{
				Essential: true,
			}

			if cd.Name != nil {
				cdView.Name = *cd.Name
			}
			if cd.Image != nil {
				cdView.Image = *cd.Image
			}
			cdView.CPU = int(cd.Cpu)
			if cd.Memory != nil {
				cdView.Memory = int(*cd.Memory)
			}
			if cd.Essential != nil {
				cdView.Essential = *cd.Essential
			}
			cdView.Command = cd.Command

			if len(cd.Environment) > 0 {
				cdView.Environment = make(map[string]string)
				for _, env := range cd.Environment {
					if env.Name != nil && env.Value != nil {
						cdView.Environment[*env.Name] = *env.Value
					}
				}
			}

			if len(cd.Secrets) > 0 {
				cdView.Secrets = make(map[string]string)
				for _, secret := range cd.Secrets {
					if secret.Name != nil && secret.ValueFrom != nil {
						cdView.Secrets[*secret.Name] = *secret.ValueFrom
					}
				}
			}

			for _, pm := range cd.PortMappings {
				pmView := PortMappingView{
					Protocol: string(pm.Protocol),
				}
				if pm.ContainerPort != nil {
					pmView.ContainerPort = int(*pm.ContainerPort)
				}
				if pm.HostPort != nil {
					pmView.HostPort = int(*pm.HostPort)
				}
				cdView.PortMappings = append(cdView.PortMappings, pmView)
			}

			view.ContainerDefinitions = append(view.ContainerDefinitions, cdView)
		}
	}

	return view
}

type ServiceView struct {
	Cluster                       string                `json:"cluster"`
	TaskDefinition                string                `json:"taskDefinition"`
	DesiredCount                  int                   `json:"desiredCount"`
	LaunchType                    string                `json:"launchType,omitempty"`
	HealthCheckGracePeriodSeconds *int                  `json:"healthCheckGracePeriodSeconds,omitempty"`
	NetworkConfiguration          *NetworkConfigView    `json:"networkConfiguration,omitempty"`
	LoadBalancers                 []LoadBalancerView    `json:"loadBalancers,omitempty"`
	ServiceRegistries             []ServiceRegistryView `json:"serviceRegistries,omitempty"`
	Deployment                    *DeploymentConfigView `json:"deployment,omitempty"`
}

type NetworkConfigView struct {
	Subnets        []string `json:"subnets"`
	SecurityGroups []string `json:"securityGroups"`
	AssignPublicIp string   `json:"assignPublicIp,omitempty"`
}

type LoadBalancerView struct {
	TargetGroupArn string `json:"targetGroupArn"`
	ContainerName  string `json:"containerName"`
	ContainerPort  int    `json:"containerPort"`
}

type ServiceRegistryView struct {
	RegistryArn   string `json:"registryArn"`
	ContainerName string `json:"containerName,omitempty"`
	ContainerPort int    `json:"containerPort,omitempty"`
	Port          int    `json:"port,omitempty"`
}

type DeploymentConfigView struct {
	Strategy               string `json:"strategy"`
	MinimumHealthyPercent  *int   `json:"minimumHealthyPercent,omitempty"`
	MaximumPercent         *int   `json:"maximumPercent,omitempty"`
	CircuitBreakerEnable   bool   `json:"circuitBreakerEnable,omitempty"`
	CircuitBreakerRollback bool   `json:"circuitBreakerRollback,omitempty"`
}

const (
	pendingTargetGroupArn  = "(known after apply)"
	pendingTaskDefRevision = "(new revision after apply)"
)

func buildServiceView(svc *resources.ServiceResource, ingress *config.Ingress, taskDefs map[string]*resources.TaskDefResource, targetGroups map[string]*resources.TargetGroupResource, manifestName string) ServiceView {
	view := ServiceView{}

	if svc.Desired != nil {
		view.Cluster = svc.Desired.Cluster
		if svc.ClusterArn != "" {
			view.Cluster = svc.ClusterArn
		}
		view.TaskDefinition = svc.TaskDefinitionArn
		view.DesiredCount = svc.Desired.DesiredCount
		view.LaunchType = svc.Desired.LaunchType
		if svc.Desired.HealthCheckGracePeriodSecondsSet {
			value := svc.Desired.HealthCheckGracePeriodSeconds
			view.HealthCheckGracePeriodSeconds = &value
		}

		if svc.Desired.NetworkConfiguration != nil {
			view.NetworkConfiguration = &NetworkConfigView{
				Subnets:        sortedStrings(svc.Desired.NetworkConfiguration.Subnets),
				SecurityGroups: sortedStrings(svc.Desired.NetworkConfiguration.SecurityGroups),
				AssignPublicIp: svc.Desired.NetworkConfiguration.AssignPublicIp,
			}
		}

		for _, lb := range svc.Desired.LoadBalancers {
			view.LoadBalancers = append(view.LoadBalancers, LoadBalancerView{
				TargetGroupArn: lb.TargetGroupArn,
				ContainerName:  lb.ContainerName,
				ContainerPort:  lb.ContainerPort,
			})
		}

		for _, reg := range svc.Desired.ServiceRegistries {
			view.ServiceRegistries = append(view.ServiceRegistries, ServiceRegistryView{
				RegistryArn:   reg.RegistryArn,
				ContainerName: reg.ContainerName,
				ContainerPort: reg.ContainerPort,
				Port:          reg.Port,
			})
		}

		addIngressLoadBalancerPlaceholders(&view, svc.Desired, ingress, svc.Name, targetGroups, manifestName)
		addTaskDefinitionPlaceholder(&view, svc.Desired, taskDefs)

		view.Deployment = &DeploymentConfigView{
			Strategy:               svc.Desired.Deployment.Strategy,
			CircuitBreakerEnable:   svc.Desired.Deployment.CircuitBreakerEnable,
			CircuitBreakerRollback: svc.Desired.Deployment.CircuitBreakerRollback,
		}
		if svc.Desired.Deployment.MinimumHealthyPercentSet {
			value := svc.Desired.Deployment.MinimumHealthyPercent
			view.Deployment.MinimumHealthyPercent = &value
		}
		if svc.Desired.Deployment.MaximumPercentSet {
			value := svc.Desired.Deployment.MaximumPercent
			view.Deployment.MaximumPercent = &value
		}
	}

	return view
}

func addTaskDefinitionPlaceholder(view *ServiceView, svc *config.Service, taskDefs map[string]*resources.TaskDefResource) {
	if view == nil || svc == nil || taskDefs == nil {
		return
	}

	tdName := svc.TaskDefinition
	if tdName == "" {
		return
	}

	td, ok := taskDefs[tdName]
	if !ok || td == nil {
		return
	}

	if td.Action == resources.TaskDefActionNoop {
		return
	}

	base := view.TaskDefinition
	if idx := strings.LastIndex(base, ":"); idx != -1 {
		base = base[:idx]
	}
	if base == "" {
		return
	}

	view.TaskDefinition = base + ":" + pendingTaskDefRevision
}

func addIngressLoadBalancerPlaceholders(view *ServiceView, svc *config.Service, ingress *config.Ingress, serviceName string, targetGroups map[string]*resources.TargetGroupResource, manifestName string) {
	if ingress == nil || svc == nil {
		return
	}

	for _, rule := range ingress.Rules {
		if rule.Service == nil || rule.Service.Name != serviceName {
			continue
		}

		containerName := rule.Service.ContainerName
		containerPort := rule.Service.ContainerPort
		if containerName == "" || containerPort <= 0 {
			continue
		}

		updated := false
		for i := range view.LoadBalancers {
			lb := &view.LoadBalancers[i]
			if lb.ContainerName == containerName && lb.ContainerPort == containerPort {
				if lb.TargetGroupArn == "" {
					lb.TargetGroupArn = resolveTargetGroupArnForView(targetGroups, manifestName, rule.Priority)
				}
				updated = true
				break
			}
		}

		if !updated {
			view.LoadBalancers = append(view.LoadBalancers, LoadBalancerView{
				TargetGroupArn: resolveTargetGroupArnForView(targetGroups, manifestName, rule.Priority),
				ContainerName:  containerName,
				ContainerPort:  containerPort,
			})
		}
	}
}

func resolveTargetGroupArn(targetGroups map[string]*resources.TargetGroupResource, manifestName string, priority int) string {
	if targetGroups == nil || manifestName == "" || priority == 0 {
		return ""
	}

	targetGroupName := fmt.Sprintf("%s-r%d", manifestName, priority)
	for _, tg := range targetGroups {
		if tg != nil && tg.Name == targetGroupName {
			return tg.Arn
		}
	}

	return ""
}

// resolveTargetGroupArnForView returns the ARN for diff view display.
// Returns placeholder only when TG is being created or recreated (new ARN will be assigned).
// For existing TGs (NOOP/UPDATE), returns the existing ARN.
func resolveTargetGroupArnForView(targetGroups map[string]*resources.TargetGroupResource, manifestName string, priority int) string {
	if targetGroups == nil || manifestName == "" || priority == 0 {
		return pendingTargetGroupArn
	}

	targetGroupName := fmt.Sprintf("%s-r%d", manifestName, priority)
	for _, tg := range targetGroups {
		if tg == nil || tg.Name != targetGroupName {
			continue
		}

		switch tg.Action {
		case resources.TargetGroupActionCreate:
			return pendingTargetGroupArn
		case resources.TargetGroupActionRecreate:
			return pendingTargetGroupArn
		default:
			// NOOP, UPDATE, DELETE - use existing ARN
			if tg.Arn != "" {
				return tg.Arn
			}
			return pendingTargetGroupArn
		}
	}

	return pendingTargetGroupArn
}

func manifestName(state *resources.DesiredState) string {
	if state == nil || state.Manifest == nil {
		return ""
	}
	return state.Manifest.Name
}

func buildServiceCurrentView(svc *resources.ServiceResource) ServiceView {
	view := ServiceView{}

	if svc.Current != nil {
		if svc.Current.ClusterArn != nil {
			view.Cluster = *svc.Current.ClusterArn
		}
		if svc.Current.TaskDefinition != nil {
			view.TaskDefinition = *svc.Current.TaskDefinition
		}
		view.DesiredCount = int(svc.Current.DesiredCount)
		view.LaunchType = string(svc.Current.LaunchType)
		if svc.Desired != nil && svc.Desired.HealthCheckGracePeriodSecondsSet && svc.Current.HealthCheckGracePeriodSeconds != nil {
			value := int(*svc.Current.HealthCheckGracePeriodSeconds)
			view.HealthCheckGracePeriodSeconds = &value
		}

		if svc.Current.NetworkConfiguration != nil && svc.Current.NetworkConfiguration.AwsvpcConfiguration != nil {
			vpc := svc.Current.NetworkConfiguration.AwsvpcConfiguration
			view.NetworkConfiguration = &NetworkConfigView{
				Subnets:        sortedStrings(vpc.Subnets),
				SecurityGroups: sortedStrings(vpc.SecurityGroups),
				AssignPublicIp: string(vpc.AssignPublicIp),
			}
		}

		for _, lb := range svc.Current.LoadBalancers {
			lbView := LoadBalancerView{}
			if lb.TargetGroupArn != nil {
				lbView.TargetGroupArn = *lb.TargetGroupArn
			}
			if lb.ContainerName != nil {
				lbView.ContainerName = *lb.ContainerName
			}
			if lb.ContainerPort != nil {
				lbView.ContainerPort = int(*lb.ContainerPort)
			}
			view.LoadBalancers = append(view.LoadBalancers, lbView)
		}

		for _, reg := range svc.Current.ServiceRegistries {
			regView := ServiceRegistryView{}
			if reg.RegistryArn != nil {
				regView.RegistryArn = *reg.RegistryArn
			}
			if reg.ContainerName != nil {
				regView.ContainerName = *reg.ContainerName
			}
			if reg.ContainerPort != nil {
				regView.ContainerPort = int(*reg.ContainerPort)
			}
			if reg.Port != nil {
				regView.Port = int(*reg.Port)
			}
			view.ServiceRegistries = append(view.ServiceRegistries, regView)
		}

		if svc.Current.DeploymentConfiguration != nil {
			dc := svc.Current.DeploymentConfiguration
			view.Deployment = &DeploymentConfigView{}
			if dc.MinimumHealthyPercent != nil {
				value := int(*dc.MinimumHealthyPercent)
				view.Deployment.MinimumHealthyPercent = &value
			}
			if dc.MaximumPercent != nil {
				value := int(*dc.MaximumPercent)
				view.Deployment.MaximumPercent = &value
			}
			if dc.DeploymentCircuitBreaker != nil {
				view.Deployment.CircuitBreakerEnable = dc.DeploymentCircuitBreaker.Enable
				view.Deployment.CircuitBreakerRollback = dc.DeploymentCircuitBreaker.Rollback
			}
		}
	}

	return view
}

type ScheduledTaskView struct {
	TaskDefinition     string                    `json:"taskDefinition"`
	Cluster            string                    `json:"cluster"`
	ScheduleExpression string                    `json:"scheduleExpression"`
	TaskCount          int                       `json:"taskCount"`
	Timezone           string                    `json:"timezone,omitempty"`
	LaunchType         string                    `json:"launchType,omitempty"`
	PlatformVersion    string                    `json:"platformVersion,omitempty"`
	Group              string                    `json:"group,omitempty"`
	NetworkConfig      *NetworkConfigView        `json:"networkConfiguration,omitempty"`
	Tags               []ScheduledTaskTagView    `json:"tags,omitempty"`
	DeadLetterConfig   *DeadLetterConfigView     `json:"deadLetterConfig,omitempty"`
	RetryPolicy        *ScheduledRetryPolicyView `json:"retryPolicy,omitempty"`
}

func buildScheduledTaskView(task *resources.ScheduledTaskResource) ScheduledTaskView {
	view := ScheduledTaskView{
		TaskDefinition:     task.TaskDefinitionArn,
		ScheduleExpression: task.ScheduleExpression(),
	}

	if task.Desired != nil {
		view.Cluster = task.Desired.Cluster
		view.TaskCount = task.Desired.TaskCount
		view.Timezone = task.Desired.Timezone
		view.LaunchType = task.Desired.LaunchType
		view.PlatformVersion = task.Desired.PlatformVersion
		view.Group = task.Desired.Group

		if task.Desired.NetworkConfiguration != nil {
			view.NetworkConfig = &NetworkConfigView{
				Subnets:        sortedStrings(task.Desired.NetworkConfiguration.Subnets),
				SecurityGroups: sortedStrings(task.Desired.NetworkConfiguration.SecurityGroups),
				AssignPublicIp: task.Desired.NetworkConfiguration.AssignPublicIp,
			}
		}

		if len(task.Desired.Tags) > 0 {
			view.Tags = buildScheduledTaskTagsView(task.Desired.Tags)
		}

		if task.Desired.DeadLetterConfig != nil && task.Desired.DeadLetterConfig.Arn != "" {
			view.DeadLetterConfig = &DeadLetterConfigView{
				Arn: task.Desired.DeadLetterConfig.Arn,
			}
		}

		if task.Desired.RetryPolicy != nil {
			view.RetryPolicy = &ScheduledRetryPolicyView{
				MaximumEventAgeInSeconds: task.Desired.RetryPolicy.MaximumEventAgeInSeconds,
				MaximumRetryAttempts:     task.Desired.RetryPolicy.MaximumRetryAttempts,
			}
		}
	}

	return view
}

type ScheduledTaskTagView struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type DeadLetterConfigView struct {
	Arn string `json:"arn"`
}

type ScheduledRetryPolicyView struct {
	MaximumEventAgeInSeconds int `json:"maximumEventAgeInSeconds,omitempty"`
	MaximumRetryAttempts     int `json:"maximumRetryAttempts,omitempty"`
}

func buildScheduledTaskTagsView(tags []config.Tag) []ScheduledTaskTagView {
	sorted := make([]ScheduledTaskTagView, 0, len(tags))
	for _, tag := range tags {
		if tag.Key == "" {
			continue
		}
		sorted = append(sorted, ScheduledTaskTagView{
			Key:   tag.Key,
			Value: tag.Value,
		})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key < sorted[j].Key
	})

	return sorted
}

type TargetGroupView struct {
	Name        string            `json:"name"`
	Port        int               `json:"port"`
	Protocol    string            `json:"protocol"`
	TargetType  string            `json:"targetType"`
	HealthCheck *HealthCheckView  `json:"healthCheck,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

type HealthCheckView struct {
	Path               string `json:"path,omitempty"`
	Protocol           string `json:"protocol,omitempty"`
	Port               string `json:"port,omitempty"`
	HealthyThreshold   int    `json:"healthyThreshold,omitempty"`
	UnhealthyThreshold int    `json:"unhealthyThreshold,omitempty"`
	Timeout            int    `json:"timeout,omitempty"`
	Interval           int    `json:"interval,omitempty"`
	Matcher            string `json:"matcher,omitempty"`
}

func buildTargetGroupView(tg *resources.TargetGroupResource) TargetGroupView {
	view := TargetGroupView{
		Name: tg.Name,
	}

	if tg.Desired != nil {
		view.Port = tg.Desired.Port
		view.Protocol = tg.Desired.Protocol
		view.TargetType = tg.Desired.TargetType
		if tg.Current == nil {
			view.Tags = tg.Desired.Tags
		}

		if tg.Desired.HealthCheck != nil {
			view.HealthCheck = &HealthCheckView{
				Path:               tg.Desired.HealthCheck.Path,
				Protocol:           tg.Desired.HealthCheck.Protocol,
				Port:               tg.Desired.HealthCheck.Port,
				HealthyThreshold:   tg.Desired.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: tg.Desired.HealthCheck.UnhealthyThreshold,
				Timeout:            tg.Desired.HealthCheck.Timeout,
				Interval:           tg.Desired.HealthCheck.Interval,
				Matcher:            tg.Desired.HealthCheck.Matcher,
			}
		}
	}

	return view
}

func buildTargetGroupCurrentView(tg *resources.TargetGroupResource) TargetGroupView {
	view := TargetGroupView{
		Name: tg.Name,
	}

	if tg.Current != nil {
		if tg.Current.Port != nil {
			view.Port = int(*tg.Current.Port)
		}
		view.Protocol = string(tg.Current.Protocol)
		view.TargetType = string(tg.Current.TargetType)

		if tg.Current.HealthCheckPath != nil {
			view.HealthCheck = &HealthCheckView{
				Path: *tg.Current.HealthCheckPath,
			}
			if tg.Current.HealthCheckProtocol != "" {
				view.HealthCheck.Protocol = string(tg.Current.HealthCheckProtocol)
			}
			if tg.Current.HealthCheckPort != nil {
				view.HealthCheck.Port = *tg.Current.HealthCheckPort
			}
			if tg.Current.HealthyThresholdCount != nil {
				view.HealthCheck.HealthyThreshold = int(*tg.Current.HealthyThresholdCount)
			}
			if tg.Current.UnhealthyThresholdCount != nil {
				view.HealthCheck.UnhealthyThreshold = int(*tg.Current.UnhealthyThresholdCount)
			}
			if tg.Current.HealthCheckTimeoutSeconds != nil {
				view.HealthCheck.Timeout = int(*tg.Current.HealthCheckTimeoutSeconds)
			}
			if tg.Current.HealthCheckIntervalSeconds != nil {
				view.HealthCheck.Interval = int(*tg.Current.HealthCheckIntervalSeconds)
			}
			if tg.Current.Matcher != nil && tg.Current.Matcher.HttpCode != nil {
				view.HealthCheck.Matcher = *tg.Current.Matcher.HttpCode
			}
		}
	}

	return view
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	sort.Strings(result)
	return result
}

type ListenerRuleView struct {
	Priority       int           `json:"priority"`
	Host           string        `json:"host,omitempty"`
	Hosts          []string      `json:"hosts,omitempty"`
	Paths          []string      `json:"paths,omitempty"`
	TargetGroupArn string        `json:"targetGroupArn,omitempty"`
	Service        *ServiceRef   `json:"service,omitempty"`
	Redirect       *RedirectView `json:"redirect,omitempty"`
}

type ServiceRef struct {
	Name          string `json:"name"`
	ContainerPort int    `json:"containerPort"`
}

type RedirectView struct {
	StatusCode string `json:"statusCode"`
	Protocol   string `json:"protocol,omitempty"`
	Host       string `json:"host,omitempty"`
	Path       string `json:"path,omitempty"`
}

func buildListenerRuleView(rule *resources.ListenerRuleResource) ListenerRuleView {
	view := ListenerRuleView{
		Priority: rule.Priority,
	}

	if rule.Desired != nil {
		view.Host = rule.Desired.Host
		view.Hosts = rule.Desired.Hosts
		view.Paths = rule.Desired.Paths

		if rule.Desired.Service != nil {
			view.Service = &ServiceRef{
				Name:          rule.Desired.Service.Name,
				ContainerPort: rule.Desired.Service.ContainerPort,
			}
			if rule.TargetGroupArn != "" {
				view.TargetGroupArn = rule.TargetGroupArn
			}
		}

		if rule.Desired.Redirect != nil {
			view.Redirect = &RedirectView{
				StatusCode: rule.Desired.Redirect.StatusCode,
				Protocol:   rule.Desired.Redirect.Protocol,
				Host:       rule.Desired.Redirect.Host,
				Path:       rule.Desired.Redirect.Path,
			}
		}
	}

	return view
}

func buildListenerRuleCurrentView(rule *resources.ListenerRuleResource) ListenerRuleView {
	view := ListenerRuleView{
		Priority: rule.Priority,
	}

	if rule.Current == nil {
		return view
	}

	if rule.Current.Priority != nil {
		if priority, err := strconv.Atoi(*rule.Current.Priority); err == nil {
			view.Priority = priority
		}
	}

	var hosts []string
	var paths []string
	for _, cond := range rule.Current.Conditions {
		if cond.Field == nil {
			continue
		}

		switch *cond.Field {
		case "host-header":
			if cond.HostHeaderConfig != nil && len(cond.HostHeaderConfig.Values) > 0 {
				hosts = append(hosts, cond.HostHeaderConfig.Values...)
			} else if len(cond.Values) > 0 {
				hosts = append(hosts, cond.Values...)
			}
		case "path-pattern":
			if cond.PathPatternConfig != nil && len(cond.PathPatternConfig.Values) > 0 {
				paths = append(paths, cond.PathPatternConfig.Values...)
			} else if len(cond.Values) > 0 {
				paths = append(paths, cond.Values...)
			}
		}
	}

	if len(hosts) == 1 {
		view.Host = hosts[0]
	} else if len(hosts) > 1 {
		view.Hosts = hosts
	}
	if len(paths) > 0 {
		view.Paths = paths
	}

	var targetGroupArn string
	for _, action := range rule.Current.Actions {
		switch string(action.Type) {
		case "forward":
			if action.TargetGroupArn != nil {
				targetGroupArn = *action.TargetGroupArn
			} else if action.ForwardConfig != nil && len(action.ForwardConfig.TargetGroups) > 0 {
				if action.ForwardConfig.TargetGroups[0].TargetGroupArn != nil {
					targetGroupArn = *action.ForwardConfig.TargetGroups[0].TargetGroupArn
				}
			}
		case "redirect":
			if action.RedirectConfig != nil {
				view.Redirect = &RedirectView{
					StatusCode: string(action.RedirectConfig.StatusCode),
				}
				if action.RedirectConfig.Protocol != nil {
					view.Redirect.Protocol = *action.RedirectConfig.Protocol
				}
				if action.RedirectConfig.Host != nil {
					view.Redirect.Host = *action.RedirectConfig.Host
				}
				if action.RedirectConfig.Path != nil {
					view.Redirect.Path = *action.RedirectConfig.Path
				}
			}
		}
	}

	if targetGroupArn != "" {
		view.TargetGroupArn = targetGroupArn
		if rule.Desired != nil && rule.Desired.Service != nil && targetGroupArn == rule.TargetGroupArn {
			view.Service = &ServiceRef{
				Name:          rule.Desired.Service.Name,
				ContainerPort: rule.Desired.Service.ContainerPort,
			}
		}
	}

	return view
}
