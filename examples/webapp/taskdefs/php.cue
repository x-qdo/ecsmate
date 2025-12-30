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
