package webapp

import "github.com/qdo/ecsmate/pkg/cue:schema"

manifest: schema.#Manifest & {
	name: "webapp"

	taskDefinitions: {
		php:   _taskdefs.php
		nginx: _taskdefs.nginx
		cron:  _taskdefs.cron
	}

	services: {
		web: {
			cluster:        _values.cluster
			taskDefinition: "php"
			desiredCount:   _values.web.replicas
			launchType:     "FARGATE"

			networkConfiguration: {
				awsvpcConfiguration: {
					subnets:        _values.network.subnets
					securityGroups: _values.network.securityGroups
					assignPublicIp: "DISABLED"
				}
			}

			loadBalancers: [{
				targetGroupArn: _values.web.targetGroupArn
				containerName:  "php"
				containerPort:  80
			}]

			deployment: {
				strategy: "rolling"
				config: {
					minimumHealthyPercent: 50
					maximumPercent:        200
					circuitBreaker: {
						enable:   true
						rollback: true
					}
				}
			}
		}

		worker: {
			cluster:        _values.cluster
			taskDefinition: "php"
			desiredCount:   _values.worker.replicas
			launchType:     "FARGATE"
			dependsOn: ["web"]

			networkConfiguration: {
				awsvpcConfiguration: {
					subnets:        _values.network.subnets
					securityGroups: _values.network.securityGroups
					assignPublicIp: "DISABLED"
				}
			}

			deployment: {
				strategy: "rolling"
				config: {
					minimumHealthyPercent: 0
					maximumPercent:        100
					circuitBreaker: {
						enable:   true
						rollback: true
					}
				}
			}
		}
	}

	scheduledTasks: {
		dailyReport: {
			taskDefinition: "cron"
			cluster:        _values.cluster
			schedule: {
				type:       "cron"
				expression: "0 2 * * ? *"
			}
			taskCount:  1
			launchType: "FARGATE"
			networkConfiguration: {
				awsvpcConfiguration: {
					subnets:        _values.network.subnets
					securityGroups: _values.network.securityGroups
					assignPublicIp: "DISABLED"
				}
			}
			overrides: {
				containerOverrides: [{
					name:    "cron"
					command: ["php", "artisan", "report:daily"]
				}]
			}
		}
	}
}

// Internal references
_values:   _
_taskdefs: _
