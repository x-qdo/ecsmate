package webapp

_values: {
	environment: "production"
	debug:       false
	logLevel:    "warning"

	cluster: "production-cluster"

	php: {
		cpu:    "512"
		memory: "1024"
	}

	nginx: {
		cpu:    "256"
		memory: "512"
	}

	cron: {
		cpu:    "512"
		memory: "1024"
	}

	network: {
		subnets: [
			"subnet-prod-1a",
			"subnet-prod-1b",
			"subnet-prod-1c",
		]
		securityGroups: [
			"sg-prod-ecs",
		]
	}

	web: {
		replicas:       5
		targetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/myapp-prod-web/1234567890123456"
	}

	worker: {
		replicas: 3
	}

	database: {
		host: "prod-db.cluster-xyz.us-east-1.rds.amazonaws.com"
		name: "myapp_production"
	}

	redis: {
		host: "prod-redis.xyz.ng.0001.use1.cache.amazonaws.com"
	}

	secrets: {
		dbPassword: "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp-prod/db-password"
		appKey:     "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp-prod/app-key"
	}
}
