package engine

import (
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

	p.planTaskDefs(state, plan)
	p.planServices(state, plan)
	p.planScheduledTasks(state, plan)

	plan.Summary = p.calculateSummary(plan.Entries)

	return plan
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
	for name, svc := range state.Services {
		entry := diff.DiffEntry{
			Name:     name,
			Resource: "Service",
		}

		switch svc.Action {
		case resources.ServiceActionCreate:
			entry.Type = diff.DiffTypeCreate
			entry.Desired = buildServiceView(svc)
			plan.Summary.Creates++

		case resources.ServiceActionUpdate:
			entry.Type = diff.DiffTypeUpdate
			entry.Current = buildServiceCurrentView(svc)
			entry.Desired = buildServiceView(svc)
			plan.Summary.Updates++

		case resources.ServiceActionRecreate:
			entry.Type = diff.DiffTypeRecreate
			entry.Current = buildServiceCurrentView(svc)
			entry.Desired = buildServiceView(svc)
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
			Name:     name,
			Resource: "ScheduledTask",
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
	Type                    string                 `json:"type"`
	Family                  string                 `json:"family,omitempty"`
	CPU                     string                 `json:"cpu,omitempty"`
	Memory                  string                 `json:"memory,omitempty"`
	NetworkMode             string                 `json:"networkMode,omitempty"`
	RequiresCompatibilities []string               `json:"requiresCompatibilities,omitempty"`
	ExecutionRoleArn        string                 `json:"executionRoleArn,omitempty"`
	TaskRoleArn             string                 `json:"taskRoleArn,omitempty"`
	ContainerDefinitions    []ContainerDefView     `json:"containerDefinitions,omitempty"`
	Arn                     string                 `json:"arn,omitempty"`
	BaseArn                 string                 `json:"baseArn,omitempty"`
}

type ContainerDefView struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	CPU         int               `json:"cpu,omitempty"`
	Memory      int               `json:"memory,omitempty"`
	Essential   bool              `json:"essential"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Secrets     map[string]string `json:"secrets,omitempty"`
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
	Cluster              string                `json:"cluster"`
	TaskDefinition       string                `json:"taskDefinition"`
	DesiredCount         int                   `json:"desiredCount"`
	LaunchType           string                `json:"launchType,omitempty"`
	NetworkConfiguration *NetworkConfigView    `json:"networkConfiguration,omitempty"`
	LoadBalancers        []LoadBalancerView    `json:"loadBalancers,omitempty"`
	Deployment           *DeploymentConfigView `json:"deployment,omitempty"`
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

type DeploymentConfigView struct {
	Strategy              string `json:"strategy"`
	MinimumHealthyPercent int    `json:"minimumHealthyPercent,omitempty"`
	MaximumPercent        int    `json:"maximumPercent,omitempty"`
	CircuitBreakerEnable  bool   `json:"circuitBreakerEnable,omitempty"`
	CircuitBreakerRollback bool  `json:"circuitBreakerRollback,omitempty"`
}

func buildServiceView(svc *resources.ServiceResource) ServiceView {
	view := ServiceView{}

	if svc.Desired != nil {
		view.Cluster = svc.Desired.Cluster
		view.TaskDefinition = svc.TaskDefinitionArn
		view.DesiredCount = svc.Desired.DesiredCount
		view.LaunchType = svc.Desired.LaunchType

		if svc.Desired.NetworkConfiguration != nil {
			view.NetworkConfiguration = &NetworkConfigView{
				Subnets:        svc.Desired.NetworkConfiguration.Subnets,
				SecurityGroups: svc.Desired.NetworkConfiguration.SecurityGroups,
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

		view.Deployment = &DeploymentConfigView{
			Strategy:               svc.Desired.Deployment.Strategy,
			MinimumHealthyPercent:  svc.Desired.Deployment.MinimumHealthyPercent,
			MaximumPercent:         svc.Desired.Deployment.MaximumPercent,
			CircuitBreakerEnable:   svc.Desired.Deployment.CircuitBreakerEnable,
			CircuitBreakerRollback: svc.Desired.Deployment.CircuitBreakerRollback,
		}
	}

	return view
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

		if svc.Current.NetworkConfiguration != nil && svc.Current.NetworkConfiguration.AwsvpcConfiguration != nil {
			vpc := svc.Current.NetworkConfiguration.AwsvpcConfiguration
			view.NetworkConfiguration = &NetworkConfigView{
				Subnets:        vpc.Subnets,
				SecurityGroups: vpc.SecurityGroups,
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

		if svc.Current.DeploymentConfiguration != nil {
			dc := svc.Current.DeploymentConfiguration
			view.Deployment = &DeploymentConfigView{}
			if dc.MinimumHealthyPercent != nil {
				view.Deployment.MinimumHealthyPercent = int(*dc.MinimumHealthyPercent)
			}
			if dc.MaximumPercent != nil {
				view.Deployment.MaximumPercent = int(*dc.MaximumPercent)
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
	TaskDefinition     string             `json:"taskDefinition"`
	Cluster            string             `json:"cluster"`
	ScheduleExpression string             `json:"scheduleExpression"`
	TaskCount          int                `json:"taskCount"`
	Timezone           string             `json:"timezone,omitempty"`
	NetworkConfig      *NetworkConfigView `json:"networkConfiguration,omitempty"`
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

		if task.Desired.NetworkConfiguration != nil {
			view.NetworkConfig = &NetworkConfigView{
				Subnets:        task.Desired.NetworkConfiguration.Subnets,
				SecurityGroups: task.Desired.NetworkConfiguration.SecurityGroups,
				AssignPublicIp: task.Desired.NetworkConfiguration.AssignPublicIp,
			}
		}
	}

	return view
}
