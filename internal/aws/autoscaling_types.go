package aws

import "time"

type RegisterScalableTargetInput struct {
	Cluster     string
	Service     string
	MinCapacity int
	MaxCapacity int
	RoleARN     *string
}

type ScalableTarget struct {
	ResourceID    string
	MinCapacity   int
	MaxCapacity   int
	RoleARN       string
	CreationTime  time.Time
	SuspendedFrom []string
}

type TargetTrackingPolicyInput struct {
	Cluster                        string
	Service                        string
	PolicyName                     string
	TargetValue                    float64
	PredefinedMetric               string
	PredefinedMetricResourceLabel  string
	CustomMetric                   *CustomMetricSpec
	ScaleInCooldown                int
	ScaleOutCooldown               int
	DisableScaleIn                 bool
}

type CustomMetricSpec struct {
	Namespace  string
	MetricName string
	Statistic  string
	Dimensions []MetricDimension
}

type MetricDimension struct {
	Name  string
	Value string
}

type StepScalingPolicyInput struct {
	Cluster                string
	Service                string
	PolicyName             string
	AdjustmentType         string
	StepAdjustments        []StepAdjustment
	Cooldown               int
	MetricAggregationType  string
	MinAdjustmentMagnitude int
}

type StepAdjustment struct {
	MetricIntervalLowerBound *float64
	MetricIntervalUpperBound *float64
	ScalingAdjustment        int
}

type ScalingPolicyInfo struct {
	PolicyName  string
	PolicyARN   string
	PolicyType  string
	ResourceID  string
	TargetValue float64
}

type ScheduledActionInput struct {
	Cluster     string
	Service     string
	ActionName  string
	Schedule    string
	Timezone    string
	StartTime   time.Time
	EndTime     time.Time
	MinCapacity int
	MaxCapacity int
}

type ScheduledActionInfo struct {
	ActionName  string
	ActionARN   string
	Schedule    string
	ResourceID  string
	MinCapacity int
	MaxCapacity int
	StartTime   time.Time
	EndTime     time.Time
}
