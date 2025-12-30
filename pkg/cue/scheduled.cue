package schema

#ScheduledTask: {
	taskDefinition: string
	cluster:        string
	taskCount?:     int & >=1 | *1

	schedule: #CronSchedule | #RateSchedule

	networkConfiguration?: #NetworkConfiguration
	launchType?:          "EC2" | "FARGATE" | "EXTERNAL"
	platformVersion?:     string
	group?:               string

	overrides?: {
		cpu?:    string
		memory?: string
		taskRoleArn?:      string
		executionRoleArn?: string
		containerOverrides?: [...{
			name:       string
			command?: [...string]
			environment?: [...#KeyValuePair]
			cpu?:       int
			memory?:    int
		}]
	}

	tags?: [...{
		key:   string
		value: string
	}]

	deadLetterConfig?: {
		arn: string
	}

	retryPolicy?: {
		maximumEventAgeInSeconds?: int & >=60 & <=86400
		maximumRetryAttempts?:     int & >=0 & <=185
	}
}

#CronSchedule: {
	type:       "cron"
	expression: string
	timezone?:  string
}

#RateSchedule: {
	type:       "rate"
	expression: string
}
