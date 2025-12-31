package schema

#Ingress: {
	listenerArn: string
	vpcId:       string

	rules: [...#IngressRule]
}

#IngressRule: {
	priority: int & >=1 & <=50000

	// Match conditions (at least one required)
	host?: string
	hosts?: [...string]
	paths?: [...string]

	// Backend: one of service, redirect, or fixedResponse
	service?: {
		name:          string // references service name in manifest
		containerName: string // container in the service's task
		containerPort: int
	}

	redirect?: {
		statusCode: "HTTP_301" | "HTTP_302" | *"HTTP_301"
		protocol?:  string
		host?:      string
		port?:      string
		path?:      string
		query?:     string
	}

	fixedResponse?: {
		statusCode:   string
		contentType?: string
		messageBody?: string
	}

	// Target group settings (only when service backend is used)
	healthCheck?: {
		path?:               string | *"/"
		protocol?:           "HTTP" | "HTTPS"
		port?:               string | *"traffic-port"
		healthyThreshold?:   int & >=2 & <=10 | *5
		unhealthyThreshold?: int & >=2 & <=10 | *2
		timeout?:            int & >=2 & <=120 | *5
		interval?:           int & >=5 & <=300 | *30
		matcher?:            string | *"200"
	}

	deregistrationDelay?: int & >=0 & <=3600 | *300
	tags?: [string]: string
}
