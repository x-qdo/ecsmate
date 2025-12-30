package webapp

_values: {
	appName:     "myapp"
	region:      "us-east-1"
	environment: "development"
	debug:       true
	logLevel:    "debug"

	cluster: "default-cluster"

	image: {
		registry: "123456789012.dkr.ecr.us-east-1.amazonaws.com"
		tag:      "latest"
	}

	executionRoleArn: "arn:aws:iam::123456789012:role/ecsTaskExecutionRole"
	taskRoleArn:      "arn:aws:iam::123456789012:role/ecsTaskRole"

	network: {
		subnets: [
			"subnet-12345678",
			"subnet-87654321",
		]
		securityGroups: [
			"sg-12345678",
		]
	}

	php: {
		cpu:    "256"
		memory: "512"
	}

	nginx: {
		cpu:    "128"
		memory: "256"
	}

	cron: {
		cpu:    "256"
		memory: "512"
	}

	web: {
		replicas:       1
		targetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/myapp-web/1234567890123456"
	}

	worker: {
		replicas: 1
	}

	database: {
		host: "localhost"
		name: "myapp"
	}

	redis: {
		host: "localhost"
	}

	secrets: {
		dbPassword: "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp/db-password"
		appKey:     "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp/app-key"
	}
}
