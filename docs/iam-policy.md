# IAM Policy for ecsmate

Minimal IAM policies following the principle of least privilege.

## Services Used

| Service | Purpose |
|---------|---------|
| ECS | Task definitions, services, clusters |
| Application Auto Scaling | Service scaling policies |
| ELBv2 | Target groups, listener rules |
| CloudWatch Logs | Log group management |
| EventBridge Scheduler | Scheduled tasks |
| IAM | Role management for scheduler |
| SSM | Parameter resolution |
| Service Discovery | Cloud Map integration |

## Full Access Policy

Required for all ecsmate operations (`diff`, `apply`, `status`, `rollback`):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECS",
      "Effect": "Allow",
      "Action": [
        "ecs:DescribeTaskDefinition",
        "ecs:DescribeServices",
        "ecs:DescribeClusters",
        "ecs:DescribeTasks",
        "ecs:ListTaskDefinitions",
        "ecs:ListTasks",
        "ecs:RegisterTaskDefinition",
        "ecs:CreateService",
        "ecs:UpdateService",
        "ecs:DeleteService",
        "ecs:RunTask"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ApplicationAutoScaling",
      "Effect": "Allow",
      "Action": [
        "application-autoscaling:RegisterScalableTarget",
        "application-autoscaling:DeregisterScalableTarget",
        "application-autoscaling:DescribeScalableTargets",
        "application-autoscaling:PutScalingPolicy",
        "application-autoscaling:DeleteScalingPolicy",
        "application-autoscaling:DescribeScalingPolicies",
        "application-autoscaling:PutScheduledAction",
        "application-autoscaling:DeleteScheduledAction",
        "application-autoscaling:DescribeScheduledActions"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ELBv2",
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancingv2:CreateTargetGroup",
        "elasticloadbalancingv2:DescribeTargetGroups",
        "elasticloadbalancingv2:DeleteTargetGroup",
        "elasticloadbalancingv2:ModifyTargetGroup",
        "elasticloadbalancingv2:ModifyTargetGroupAttributes",
        "elasticloadbalancingv2:AddTags",
        "elasticloadbalancingv2:DescribeTags",
        "elasticloadbalancingv2:CreateRule",
        "elasticloadbalancingv2:DescribeRules",
        "elasticloadbalancingv2:DeleteRule",
        "elasticloadbalancingv2:ModifyRule"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:DescribeLogGroups",
        "logs:DeleteLogGroup",
        "logs:PutRetentionPolicy",
        "logs:TagResource",
        "logs:GetLogEvents"
      ],
      "Resource": "*"
    },
    {
      "Sid": "EventBridgeScheduler",
      "Effect": "Allow",
      "Action": [
        "scheduler:GetSchedule",
        "scheduler:ListSchedules",
        "scheduler:CreateSchedule",
        "scheduler:UpdateSchedule",
        "scheduler:DeleteSchedule",
        "scheduler:CreateScheduleGroup",
        "scheduler:ListTagsForResource",
        "scheduler:TagResource",
        "scheduler:UntagResource"
      ],
      "Resource": "*"
    },
    {
      "Sid": "IAMForScheduler",
      "Effect": "Allow",
      "Action": [
        "iam:GetRole",
        "iam:CreateRole",
        "iam:PutRolePolicy"
      ],
      "Resource": "arn:aws:iam::*:role/ecsmate-*"
    },
    {
      "Sid": "SSM",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParameters"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ServiceDiscovery",
      "Effect": "Allow",
      "Action": [
        "servicediscovery:CreateService",
        "servicediscovery:GetService",
        "servicediscovery:UpdateService",
        "servicediscovery:DeleteService",
        "servicediscovery:ListServices",
        "servicediscovery:ListTagsForResource"
      ],
      "Resource": "*"
    },
    {
      "Sid": "PassRole",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "iam:PassedToService": [
            "ecs-tasks.amazonaws.com",
            "scheduler.amazonaws.com"
          ]
        }
      }
    }
  ]
}
```

## Read-Only Policy

For `diff`, `status`, and `validate` commands only:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECSRead",
      "Effect": "Allow",
      "Action": [
        "ecs:DescribeTaskDefinition",
        "ecs:DescribeServices",
        "ecs:DescribeClusters",
        "ecs:DescribeTasks",
        "ecs:ListTaskDefinitions",
        "ecs:ListTasks"
      ],
      "Resource": "*"
    },
    {
      "Sid": "AutoScalingRead",
      "Effect": "Allow",
      "Action": [
        "application-autoscaling:DescribeScalableTargets",
        "application-autoscaling:DescribeScalingPolicies",
        "application-autoscaling:DescribeScheduledActions"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ELBv2Read",
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancingv2:DescribeTargetGroups",
        "elasticloadbalancingv2:DescribeTags",
        "elasticloadbalancingv2:DescribeRules"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchLogsRead",
      "Effect": "Allow",
      "Action": [
        "logs:DescribeLogGroups",
        "logs:GetLogEvents"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SchedulerRead",
      "Effect": "Allow",
      "Action": [
        "scheduler:GetSchedule",
        "scheduler:ListSchedules",
        "scheduler:ListTagsForResource"
      ],
      "Resource": "*"
    },
    {
      "Sid": "IAMRead",
      "Effect": "Allow",
      "Action": "iam:GetRole",
      "Resource": "*"
    },
    {
      "Sid": "SSMRead",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParameters"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ServiceDiscoveryRead",
      "Effect": "Allow",
      "Action": [
        "servicediscovery:GetService",
        "servicediscovery:ListServices",
        "servicediscovery:ListTagsForResource"
      ],
      "Resource": "*"
    }
  ]
}
```

## Resource-Scoped Policy (Production)

Replace `REGION`, `ACCOUNT_ID`, `CLUSTER_NAME` with actual values:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECSRead",
      "Effect": "Allow",
      "Action": [
        "ecs:DescribeTaskDefinition",
        "ecs:DescribeServices",
        "ecs:DescribeClusters",
        "ecs:DescribeTasks",
        "ecs:ListTaskDefinitions",
        "ecs:ListTasks"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ECSWriteClusterScoped",
      "Effect": "Allow",
      "Action": [
        "ecs:CreateService",
        "ecs:UpdateService",
        "ecs:DeleteService",
        "ecs:RunTask"
      ],
      "Resource": [
        "arn:aws:ecs:REGION:ACCOUNT_ID:cluster/CLUSTER_NAME",
        "arn:aws:ecs:REGION:ACCOUNT_ID:service/CLUSTER_NAME/*"
      ]
    },
    {
      "Sid": "ECSTaskDefinitions",
      "Effect": "Allow",
      "Action": "ecs:RegisterTaskDefinition",
      "Resource": "*"
    },
    {
      "Sid": "ApplicationAutoScaling",
      "Effect": "Allow",
      "Action": [
        "application-autoscaling:RegisterScalableTarget",
        "application-autoscaling:DeregisterScalableTarget",
        "application-autoscaling:DescribeScalableTargets",
        "application-autoscaling:PutScalingPolicy",
        "application-autoscaling:DeleteScalingPolicy",
        "application-autoscaling:DescribeScalingPolicies",
        "application-autoscaling:PutScheduledAction",
        "application-autoscaling:DeleteScheduledAction",
        "application-autoscaling:DescribeScheduledActions"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ELBv2",
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancingv2:CreateTargetGroup",
        "elasticloadbalancingv2:DescribeTargetGroups",
        "elasticloadbalancingv2:DeleteTargetGroup",
        "elasticloadbalancingv2:ModifyTargetGroup",
        "elasticloadbalancingv2:ModifyTargetGroupAttributes",
        "elasticloadbalancingv2:AddTags",
        "elasticloadbalancingv2:DescribeTags",
        "elasticloadbalancingv2:CreateRule",
        "elasticloadbalancingv2:DescribeRules",
        "elasticloadbalancingv2:DeleteRule",
        "elasticloadbalancingv2:ModifyRule"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:DescribeLogGroups",
        "logs:DeleteLogGroup",
        "logs:PutRetentionPolicy",
        "logs:TagResource",
        "logs:GetLogEvents"
      ],
      "Resource": "arn:aws:logs:REGION:ACCOUNT_ID:log-group:/ecs/*"
    },
    {
      "Sid": "EventBridgeScheduler",
      "Effect": "Allow",
      "Action": [
        "scheduler:GetSchedule",
        "scheduler:ListSchedules",
        "scheduler:CreateSchedule",
        "scheduler:UpdateSchedule",
        "scheduler:DeleteSchedule",
        "scheduler:CreateScheduleGroup",
        "scheduler:ListTagsForResource",
        "scheduler:TagResource",
        "scheduler:UntagResource"
      ],
      "Resource": [
        "arn:aws:scheduler:REGION:ACCOUNT_ID:schedule-group/*",
        "arn:aws:scheduler:REGION:ACCOUNT_ID:schedule/*"
      ]
    },
    {
      "Sid": "IAMForScheduler",
      "Effect": "Allow",
      "Action": [
        "iam:GetRole",
        "iam:CreateRole",
        "iam:PutRolePolicy"
      ],
      "Resource": "arn:aws:iam::ACCOUNT_ID:role/ecsmate-*"
    },
    {
      "Sid": "SSM",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParameters"
      ],
      "Resource": "arn:aws:ssm:REGION:ACCOUNT_ID:parameter/*"
    },
    {
      "Sid": "ServiceDiscovery",
      "Effect": "Allow",
      "Action": [
        "servicediscovery:CreateService",
        "servicediscovery:GetService",
        "servicediscovery:UpdateService",
        "servicediscovery:DeleteService",
        "servicediscovery:ListServices",
        "servicediscovery:ListTagsForResource"
      ],
      "Resource": "*"
    },
    {
      "Sid": "PassRole",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": [
        "arn:aws:iam::ACCOUNT_ID:role/ecs-task-*",
        "arn:aws:iam::ACCOUNT_ID:role/ecsmate-*"
      ],
      "Condition": {
        "StringEquals": {
          "iam:PassedToService": [
            "ecs-tasks.amazonaws.com",
            "scheduler.amazonaws.com"
          ]
        }
      }
    }
  ]
}
```

## Notes

- `ecs:RegisterTaskDefinition` cannot be resource-scoped (AWS limitation)
- `application-autoscaling` permissions cannot be resource-scoped (AWS limitation)
- `iam:PassRole` is required for ECS task execution roles and EventBridge Scheduler
- IAM role creation is scoped to `ecsmate-*` prefix (roles created by ecsmate for scheduler)
