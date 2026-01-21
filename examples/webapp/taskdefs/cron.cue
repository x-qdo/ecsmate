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

		environment: {
			APP_ENV:     _values.environment
			APP_DEBUG:   "\(_values.debug)"
			LOG_LEVEL:   _values.logLevel
			DB_HOST:     _values.database.host
			DB_DATABASE: _values.database.name
			REDIS_HOST:  _values.redis.host
		}

		secrets: {
			DB_PASSWORD: _values.secrets.dbPassword
			APP_KEY:     _values.secrets.appKey
		}

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
