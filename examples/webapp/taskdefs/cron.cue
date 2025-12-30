package webapp

_taskdefs: cron: {
	type:   "managed"
	family: "\(_values.appName)-cron"

	cpu:                    _values.cron.cpu
	memory:                 _values.cron.memory
	networkMode:            "awsvpc"
	requiresCompatibilities: ["FARGATE"]
	executionRoleArn:       _values.executionRoleArn
	taskRoleArn:            _values.taskRoleArn

	containerDefinitions: [{
		name:      "cron"
		image:     "\(_values.image.registry)/php:\(_values.image.tag)"
		cpu:       0
		essential: true

		environment: [
			{name: "APP_ENV", value:      _values.environment},
			{name: "APP_DEBUG", value:    "\(_values.debug)"},
			{name: "LOG_LEVEL", value:    _values.logLevel},
			{name: "DB_HOST", value:      _values.database.host},
			{name: "DB_DATABASE", value:  _values.database.name},
			{name: "REDIS_HOST", value:   _values.redis.host},
		]

		secrets: [
			{name: "DB_PASSWORD", valueFrom:  _values.secrets.dbPassword},
			{name: "APP_KEY", valueFrom:      _values.secrets.appKey},
		]

		logConfiguration: {
			logDriver: "awslogs"
			options: {
				"awslogs-group":         "/ecs/\(_values.appName)-cron"
				"awslogs-region":        _values.region
				"awslogs-stream-prefix": "cron"
			}
		}

		// Command will be overridden per scheduled task
		command: ["php", "artisan", "schedule:run"]
	}]
}
