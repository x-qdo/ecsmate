package webapp

_values: {
	environment: "staging"
	debug:       false
	logLevel:    "info"

	cluster: "staging-cluster"

	network: {
		subnets: [
			"subnet-staging-1",
			"subnet-staging-2",
		]
		securityGroups: [
			"sg-staging",
		]
	}

	web: {
		replicas:       2
		targetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/myapp-staging-web/1234567890123456"
	}

	worker: {
		replicas: 1
	}

	database: {
		host: "staging-db.cluster-xyz.us-east-1.rds.amazonaws.com"
		name: "myapp_staging"
	}

	redis: {
		host: "staging-redis.xyz.ng.0001.use1.cache.amazonaws.com"
	}

	secrets: {
		dbPassword: "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp-staging/db-password"
		appKey:     "arn:aws:secretsmanager:us-east-1:123456789012:secret:myapp-staging/app-key"
	}
}
