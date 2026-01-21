package webapp

import (
	"list"
	"strings"

	"github.com/x-qdo/ecsmate/pkg/cue:schema"
)

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

	_cronTasks: [{
		name:    "dailyReport"
		command: "report:daily"
		cron:    "0 2 * * ? *"
	}, {
		name:    "weeklyCleanup"
		command: "cleanup:old-records --days=30"
		cron:    "0 3 ? * SUN *"
	}]

	scheduledTasks: {
		for _, task in _cronTasks {
			(task.name): {
				taskDefinition: "cron"
				cluster:        _values.cluster
				schedule: {
					type:       "cron"
					expression: task.cron
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
						command: list.Concat([["php", "artisan"], strings.Split(task.command, " ")])
					}]
				}
			}
		}
	}
}

// Internal references
_values:   _
_taskdefs: _
