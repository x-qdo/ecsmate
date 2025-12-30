package webapp

_taskdefs: nginx: {
	type:   "managed"
	family: "\(_values.appName)-nginx"

	cpu:                    _values.nginx.cpu
	memory:                 _values.nginx.memory
	networkMode:            "awsvpc"
	requiresCompatibilities: ["FARGATE"]
	executionRoleArn:       _values.executionRoleArn

	containerDefinitions: [{
		name:      "nginx"
		image:     "\(_values.image.registry)/nginx:\(_values.image.tag)"
		cpu:       0
		essential: true

		portMappings: [{
			containerPort: 80
			protocol:      "tcp"
		}]

		environment: [
			{name: "UPSTREAM_HOST", value: "localhost"},
			{name: "UPSTREAM_PORT", value: "9000"},
		]

		logConfiguration: {
			logDriver: "awslogs"
			options: {
				"awslogs-group":         "/ecs/\(_values.appName)-nginx"
				"awslogs-region":        _values.region
				"awslogs-stream-prefix": "nginx"
			}
		}
	}]
}
