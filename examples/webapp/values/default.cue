package webapp

_values: {
	appName:     string | *"myapp"
	region:      string | *"us-east-1"
	environment: string | *"development"
	debug:       bool | *true
	logLevel:    string | *"debug"

	cluster: string | *"default-cluster"

	image: {
		registry: string | *"123456789012.dkr.ecr.us-east-1.amazonaws.com"
		tag:      string | *"latest"
	}

	executionRoleArn: string | *"arn:aws:iam::123456789012:role/ecsTaskExecutionRole"
	taskRoleArn:      string | *"arn:aws:iam::123456789012:role/ecsTaskRole"

	network: {
		subnets: [...string] | *[
			"subnet-12345678",
			"subnet-87654321",
		]
		securityGroups: [...string] | *[
			"sg-12345678",
		]
	}

	php: {
		cpu:    string | *"256"
		memory: string | *"512"
	}

	nginx: {
		cpu:    string | *"128"
		memory: string | *"256"
	}

	cron: {
		cpu:    string | *"256"
		memory: string | *"512"
	}

	web: {
		replicas:       int | *1
		targetGroupArn: string | *"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/myapp-web/1234567890123456"
	}

	worker: {
		replicas: int | *1
	}

	database: {
		host: string | *"localhost"
		name: string | *"myapp"
	}

	redis: {
		host: string | *"localhost"
	}

	secrets: {
		dbPassword: string | *"arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp/db-password"
		appKey:     string | *"arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp/app-key"
	}
}
