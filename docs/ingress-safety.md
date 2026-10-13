# Ingress safety

ECSMate checks shared ALB routing in both `diff` and `apply`. Conflicts and failed ownership reads exit with code 1.

## Ownership and matching

- Set manifest tags that distinguish the instance, including its environment. At least one nonempty tag beyond `ManagedBy` is required. ECSMate compares all manifest tags; it does not assume specific tag names such as `Tenant` or `Application`.
- Target-group ownership requires both the manifest naming pattern and matching tags, including `ManagedBy=ecsmate`. A matching name, host, or priority alone is not proof of ownership.
- New target groups and listener rules inherit manifest tags, merged with rule tags. Contradictory rule tags are rejected. Existing resources are not automatically retagged.
- Adding or changing a manifest tag (including metadata such as `CostCenter`) blocks existing resources until their tags match. Errors identify missing tags and conflicting values. Verify the actual owner before manually updating tags on that instance's target groups and already-tagged listener rules; ECSMate does not adopt resources just because a tag is missing.
- Legacy untagged forwarding rules can be managed through their verified target group. Redirect and fixed-response rules need their own matching ownership tags.
- Generated target-group names must fit the AWS 32-character limit. Names are no longer silently truncated.

## Conflicts and protected rules

A priority occupied by another instance or an unmanageable rule blocks deployment. ECSMate does not pick the next free priority.

If an owned rule already uses another priority, ECSMate preserves its actual priority and target-group ARN and warns about drift, provided the requested priority is free. Update the manifest to the live priority to remove the warning. If the requested priority belongs to another instance, correct the manifest before deploying.

Hostnames cannot overlap another instance's forwarding rules, even on separate paths. Comparison is case-insensitive and includes all aliases. Wildcards match literal hostnames; two wildcard patterns are conservatively treated as overlapping. This also blocks overlapping non-default wildcard fallback rules, regardless of priority. The listener's default rule is excluded. Rules within the same instance can share a host for different paths, such as web and Jaeger.

Rules with additional conditions, authentication, multiple actions, weighted multi-target forwarding, enabled target-group stickiness, regex conditions, or transforms are never adopted, overwritten, or deleted. Overlapping protected forwarding rules require manual review. Unrelated unknown rules and infrastructure deny/redirect rules are left alone; they still reserve their priorities. Multiple eligible matches for one manifest rule are rejected.

Orphan cleanup uses the same ownership checks and keeps target groups referenced by protected rules.

## Apply and limitations

The initial ingress check runs before scheduler IAM role creation. The executor checks again before its first write, including with `--auto-approve`, and refuses a stale plan. Execution uses the checked resource identities instead of looking them up again by name or priority. A target group returned by creation is ownership-checked before further modification or attachment.

The guard also checks existing ECS service target-group bindings for ingress-managed container/port pairs. It does not audit every registered task or repair historical cross-instance registrations.

These checks are not a distributed lock. Serialize deployment writers sharing an ALB, including writers in different projects or environments. A writer changing routing after the final check can still race with a deployment. AWS priority uniqueness alone does not prevent hostname or target-group-name races.

## Before upgrading

1. Verify deployment roles allow `elasticloadbalancing:DescribeTags` and creation with `elasticloadbalancing:AddTags` for target groups and rules. See [IAM policy](iam-policy.md).
2. Audit manifest ownership tags, generated names, and requested priorities against live rules, including any priorities shifted by earlier versions.
3. Run `validate` and `diff` for each consumer, environment, and region before enabling deployments. Resolve reported conflicts deliberately; do not relabel another instance's resources to bypass the guard.
4. Confirm previously shared target groups have been separated, including their registered tasks, and arrange shared-ALB deployment serialization.

An ingress-check failure does not repair or roll back existing routing. The initial check prevents early writes for conflicts already visible then; later failures do not undo earlier successful operations. Also, `diff` may still create a missing scheduler IAM role after the initial guard passes.
