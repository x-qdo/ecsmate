package webapp

_taskdefs: php: {
	type:   "managed"
	family: "\(_values.appName)-php"

	cpu:                    _values.php.cpu
	memory:                 _values.php.memory
	networkMode:            "awsvpc"
	requiresCompatibilities: ["FARGATE"]
	executionRoleArn:       _values.executionRoleArn
	taskRoleArn:            _values.taskRoleArn

	containerDefinitions: [{
		name:      "php"
		image:     "\(_values.image.registry)/php:\(_values.image.tag)"
		cpu:       0
		essential: true

		portMappings: [{
			containerPort: 80
			protocol:      "tcp"
		}]

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
				"awslogs-group":         "/ecs/\(_values.appName)-php"
				"awslogs-region":        _values.region
				"awslogs-stream-prefix": "php"
			}
		}

		healthCheck: {
			command:     ["CMD-SHELL", "curl -f http://localhost/health || exit 1"]
			interval:    30
			timeout:     5
			retries:     3
			startPeriod: 60
		}
	}]
}
