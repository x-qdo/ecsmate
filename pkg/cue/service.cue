package schema

#Service: {
	cluster:        string
	taskDefinition: string
	desiredCount:   int & >=0

	launchType?:          "EC2" | "FARGATE" | "EXTERNAL"
	capacityProviderStrategy?: [...#CapacityProviderStrategyItem]
	platformVersion?:     string
	enableExecuteCommand?: bool
	healthCheckGracePeriodSeconds?: int & >=0

	networkConfiguration?: #NetworkConfiguration
	loadBalancers?: [...#LoadBalancer]
	serviceRegistries?: [...#ServiceRegistry]

	deployment:    #DeploymentConfiguration
	dependsOn?: [...string]

	autoScaling?: #AutoScaling
}

#CapacityProviderStrategyItem: {
	capacityProvider: string
	weight:           int & >=0 & <=1000
	base?:            int & >=0 & <=100000
}

#NetworkConfiguration: {
	awsvpcConfiguration: {
		subnets: [...string]
		securityGroups?: [...string]
		assignPublicIp?: "ENABLED" | "DISABLED"
	}
}

#LoadBalancer: {
	targetGroupArn: string
	containerName:  string
	containerPort:  int
}

#ServiceRegistry: {
	registryArn:     string
	containerName?:  string
	containerPort?:  int
	port?:           int
}

#DeploymentConfiguration: #RollingDeployment | #BlueGreenDeployment | #CanaryDeployment

#RollingDeployment: {
	strategy: "rolling"
	config: {
		minimumHealthyPercent?: int & >=0 & <=100
		maximumPercent?:        int & >=100 & <=200
		circuitBreaker?: {
			enable:    bool
			rollback?: bool
		}
	}
}

#BlueGreenDeployment: {
	strategy: "blue-green"
	config: {
		codeDeployApp:        string
		deploymentGroup:      string
		terminationWaitTime?: int
		deploymentConfig?:    string
	}
}

#CanaryDeployment: {
	strategy: "canary"
	config: {
		steps: [...#CanaryStep]
	}
}

#CanaryStep: {
	traffic: int & >=0 & <=100
	wait?:   string
}

#AutoScaling: {
	minCapacity: int & >=0
	maxCapacity: int & >=1

	policies: [...#ScalingPolicy]
}

#ScalingPolicy: #TargetTrackingPolicy | #StepScalingPolicy

#TargetTrackingPolicy: {
	name:        string
	type:        "TargetTrackingScaling"
	targetValue: number

	predefinedMetric?: "ECSServiceAverageCPUUtilization" | "ECSServiceAverageMemoryUtilization" | "ALBRequestCountPerTarget"
	customMetric?: {
		namespace:  string
		metricName: string
		dimensions?: [...{
			name:  string
			value: string
		}]
		statistic?: string
	}

	scaleInCooldown?:  int
	scaleOutCooldown?: int
}

#StepScalingPolicy: {
	name: string
	type: "StepScaling"

	adjustmentType:       "ChangeInCapacity" | "ExactCapacity" | "PercentChangeInCapacity"
	metricAggregationType?: "Average" | "Minimum" | "Maximum"
	cooldown?:            int
	minAdjustmentMagnitude?: int

	stepAdjustments: [...{
		metricIntervalLowerBound?: number
		metricIntervalUpperBound?: number
		scalingAdjustment:         int
	}]
}
