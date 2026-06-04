package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

type LogGroupAction string

const (
	LogGroupActionCreate LogGroupAction = "CREATE"
	LogGroupActionUpdate LogGroupAction = "UPDATE"
	LogGroupActionDelete LogGroupAction = "DELETE"
	LogGroupActionNoop   LogGroupAction = "NOOP"

	managedSubscriptionFiltersTag = "ecsmate:ManagedSubscriptionFilters"
)

// LogGroupSpec represents the desired state for a log group (extracted from LogConfiguration)
type LogGroupSpec struct {
	Name                string
	RetentionInDays     int
	KMSKeyID            string
	Tags                map[string]string
	SubscriptionFilters []SubscriptionFilterSpec
}

type SubscriptionFilterSpec struct {
	Name           string
	DestinationArn string
	FilterPattern  string
	RoleArn        string
	Distribution   string
}

type LogGroupResource struct {
	Name        string
	Desired     *LogGroupSpec
	Current     *types.LogGroup
	CurrentTags map[string]string
	Action      LogGroupAction
}

type logGroupClient interface {
	CreateLogGroup(ctx context.Context, input *awsclient.CreateLogGroupInput) error
	SetRetentionPolicy(ctx context.Context, logGroupName string, retentionDays int) error
	DescribeLogGroup(ctx context.Context, logGroupName string) (*types.LogGroup, error)
	DeleteLogGroup(ctx context.Context, logGroupName string) error
	TagLogGroup(ctx context.Context, logGroupName string, tags map[string]string) error
	ListLogGroupTags(ctx context.Context, logGroupName string) (map[string]string, error)
	DescribeSubscriptionFilters(ctx context.Context, logGroupName string) ([]types.SubscriptionFilter, error)
	PutSubscriptionFilter(ctx context.Context, input *awsclient.PutSubscriptionFilterInput) error
	DeleteSubscriptionFilter(ctx context.Context, logGroupName, filterName string) error
}

type LogGroupManager struct {
	client logGroupClient
}

func NewLogGroupManager(client logGroupClient) *LogGroupManager {
	return &LogGroupManager{
		client: client,
	}
}

// ExtractLogGroups extracts log group specs from task definitions where createLogGroup is true
func ExtractLogGroups(manifest *config.Manifest) map[string]*LogGroupSpec {
	logGroups := make(map[string]*LogGroupSpec)
	if manifest == nil {
		return logGroups
	}

	taskDefNames := make([]string, 0, len(manifest.TaskDefinitions))
	for name := range manifest.TaskDefinitions {
		taskDefNames = append(taskDefNames, name)
	}
	sort.Strings(taskDefNames)

	for _, taskDefName := range taskDefNames {
		td := manifest.TaskDefinitions[taskDefName]
		for _, container := range td.ContainerDefinitions {
			if container.LogConfiguration == nil {
				continue
			}
			lc := container.LogConfiguration

			// Only process awslogs driver with createLogGroup enabled
			if lc.LogDriver != "awslogs" || !lc.CreateLogGroup {
				continue
			}

			logGroupName := lc.Options["awslogs-group"]
			if logGroupName == "" {
				continue
			}

			spec, exists := logGroups[logGroupName]
			if !exists {
				spec = &LogGroupSpec{
					Name:            logGroupName,
					RetentionInDays: lc.RetentionInDays,
					KMSKeyID:        lc.KMSKeyID,
					Tags:            lc.LogGroupTags,
				}
				logGroups[logGroupName] = spec
			}
			addSubscriptionFilters(spec, lc.SubscriptionFilters)
		}
	}

	return logGroups
}

func addSubscriptionFilters(spec *LogGroupSpec, filters []config.SubscriptionFilter) {
	if spec == nil || len(filters) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(spec.SubscriptionFilters))
	for _, filter := range spec.SubscriptionFilters {
		seen[filter.Name] = struct{}{}
	}

	for _, filter := range filters {
		if filter.Name == "" || filter.DestinationArn == "" {
			continue
		}
		if _, exists := seen[filter.Name]; exists {
			continue
		}

		spec.SubscriptionFilters = append(spec.SubscriptionFilters, SubscriptionFilterSpec{
			Name:           filter.Name,
			DestinationArn: filter.DestinationArn,
			FilterPattern:  filter.FilterPattern,
			RoleArn:        filter.RoleArn,
			Distribution:   filter.Distribution,
		})
		seen[filter.Name] = struct{}{}
	}
}

func (m *LogGroupManager) BuildResource(ctx context.Context, spec *LogGroupSpec) (*LogGroupResource, error) {
	resource := &LogGroupResource{
		Name:    spec.Name,
		Desired: spec,
	}

	if err := m.discoverLogGroup(ctx, resource); err != nil {
		log.Debug("failed to discover log group", "name", spec.Name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *LogGroupManager) discoverLogGroup(ctx context.Context, resource *LogGroupResource) error {
	if resource.Desired == nil {
		return nil
	}

	log.Debug("discovering log group", "name", resource.Desired.Name)

	lg, err := m.client.DescribeLogGroup(ctx, resource.Desired.Name)
	if err != nil {
		return err
	}

	resource.Current = lg
	if lg == nil {
		return nil
	}

	tags, err := m.client.ListLogGroupTags(ctx, resource.Desired.Name)
	if err != nil {
		return fmt.Errorf("failed to list log group tags: %w", err)
	}
	resource.CurrentTags = tags
	return nil
}

func (resource *LogGroupResource) determineAction() {
	if resource.Desired == nil {
		if resource.Current != nil {
			resource.Action = LogGroupActionDelete
		} else {
			resource.Action = LogGroupActionNoop
		}
		return
	}

	if resource.Current == nil {
		resource.Action = LogGroupActionCreate
		return
	}

	// Check if retention needs update
	currentRetention := 0
	if resource.Current.RetentionInDays != nil {
		currentRetention = int(aws.ToInt32(resource.Current.RetentionInDays))
	}

	if resource.Desired.RetentionInDays != currentRetention && resource.Desired.RetentionInDays > 0 {
		resource.Action = LogGroupActionUpdate
		return
	}

	resource.Action = LogGroupActionNoop
}

func (m *LogGroupManager) Create(ctx context.Context, resource *LogGroupResource) error {
	log.Info("creating log group", "name", resource.Desired.Name)

	if err := m.client.CreateLogGroup(ctx, &awsclient.CreateLogGroupInput{
		Name:     resource.Desired.Name,
		KMSKeyID: resource.Desired.KMSKeyID,
		Tags:     userLogGroupTags(resource.Desired.Tags),
	}); err != nil {
		return err
	}

	// Set retention policy if specified
	if resource.Desired.RetentionInDays > 0 {
		if err := m.client.SetRetentionPolicy(ctx, resource.Desired.Name, resource.Desired.RetentionInDays); err != nil {
			return fmt.Errorf("failed to set retention policy: %w", err)
		}
	}

	return m.reconcileSubscriptionFilters(ctx, resource)
}

func (m *LogGroupManager) Update(ctx context.Context, resource *LogGroupResource) error {
	log.Info("updating log group", "name", resource.Desired.Name)

	// Update retention policy
	if resource.Desired.RetentionInDays > 0 {
		if err := m.client.SetRetentionPolicy(ctx, resource.Desired.Name, resource.Desired.RetentionInDays); err != nil {
			return fmt.Errorf("failed to update retention policy: %w", err)
		}
	}

	// Update tags if needed
	if tags := userLogGroupTags(resource.Desired.Tags); len(tags) > 0 {
		if err := m.client.TagLogGroup(ctx, resource.Desired.Name, tags); err != nil {
			return fmt.Errorf("failed to update tags: %w", err)
		}
	}

	return m.reconcileSubscriptionFilters(ctx, resource)
}

func (m *LogGroupManager) Delete(ctx context.Context, resource *LogGroupResource) error {
	log.Info("deleting log group", "name", resource.Current.LogGroupName)
	return m.client.DeleteLogGroup(ctx, aws.ToString(resource.Current.LogGroupName))
}

func (m *LogGroupManager) Apply(ctx context.Context, resource *LogGroupResource) error {
	switch resource.Action {
	case LogGroupActionCreate:
		return m.Create(ctx, resource)
	case LogGroupActionUpdate:
		return m.Update(ctx, resource)
	case LogGroupActionDelete:
		return m.Delete(ctx, resource)
	case LogGroupActionNoop:
		log.Debug("no log group metadata changes detected", "name", resource.Name)
		return m.reconcileSubscriptionFilters(ctx, resource)
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}

func (m *LogGroupManager) NeedsSubscriptionReconcile(ctx context.Context, resource *LogGroupResource) (bool, error) {
	if resource == nil || resource.Desired == nil {
		return false, nil
	}
	if len(resource.Desired.SubscriptionFilters) > 0 {
		return true, nil
	}

	managedNames, err := m.currentManagedSubscriptionFilterNames(ctx, resource)
	if err != nil {
		return false, err
	}
	return len(managedNames) > 0, nil
}

func (m *LogGroupManager) reconcileSubscriptionFilters(ctx context.Context, resource *LogGroupResource) error {
	if resource == nil || resource.Desired == nil {
		return nil
	}

	logGroupName := resource.Desired.Name
	desiredByName, desiredNames, err := desiredSubscriptionFiltersByName(resource.Desired.SubscriptionFilters)
	if err != nil {
		return fmt.Errorf("invalid subscription filters for log group %s: %w", logGroupName, err)
	}

	managedNames, err := m.currentManagedSubscriptionFilterNames(ctx, resource)
	if err != nil {
		return fmt.Errorf("failed to read managed subscription filter state: %w", err)
	}

	if len(desiredByName) == 0 && len(managedNames) == 0 {
		return nil
	}

	currentFilters, err := m.client.DescribeSubscriptionFilters(ctx, logGroupName)
	if err != nil {
		return fmt.Errorf("failed to describe subscription filters: %w", err)
	}

	currentByName := make(map[string]types.SubscriptionFilter, len(currentFilters))
	for _, filter := range currentFilters {
		currentByName[aws.ToString(filter.FilterName)] = filter
	}

	for _, desired := range resource.Desired.SubscriptionFilters {
		current, exists := currentByName[desired.Name]
		if exists && subscriptionFilterMatches(desired, current) {
			log.Debug("subscription filter unchanged", "logGroup", logGroupName, "name", desired.Name)
			continue
		}

		action := "creating"
		if exists {
			action = "updating"
		}
		log.Info(action+" subscription filter", "logGroup", logGroupName, "name", desired.Name)

		if err := m.client.PutSubscriptionFilter(ctx, &awsclient.PutSubscriptionFilterInput{
			LogGroupName:   logGroupName,
			Name:           desired.Name,
			DestinationArn: desired.DestinationArn,
			FilterPattern:  desired.FilterPattern,
			RoleArn:        desired.RoleArn,
			Distribution:   desired.Distribution,
		}); err != nil {
			return fmt.Errorf("failed to %s subscription filter %s: %w", action, desired.Name, err)
		}
	}

	managedNameList := make([]string, 0, len(managedNames))
	for name := range managedNames {
		managedNameList = append(managedNameList, name)
	}
	sort.Strings(managedNameList)

	for _, name := range managedNameList {
		if _, stillDesired := desiredByName[name]; stillDesired {
			continue
		}
		if _, exists := currentByName[name]; !exists {
			continue
		}

		log.Info("deleting subscription filter", "logGroup", logGroupName, "name", name)
		if err := m.client.DeleteSubscriptionFilter(ctx, logGroupName, name); err != nil {
			return fmt.Errorf("failed to delete subscription filter %s: %w", name, err)
		}
	}

	if err := m.tagManagedSubscriptionFilters(ctx, logGroupName, desiredNames); err != nil {
		return err
	}

	return nil
}

func (m *LogGroupManager) currentManagedSubscriptionFilterNames(ctx context.Context, resource *LogGroupResource) (map[string]struct{}, error) {
	if resource == nil {
		return map[string]struct{}{}, nil
	}

	if resource.CurrentTags == nil && resource.Current != nil {
		tags, err := m.client.ListLogGroupTags(ctx, resource.Name)
		if err != nil {
			return nil, err
		}
		resource.CurrentTags = tags
	}

	return parseManagedSubscriptionFilterNames(resource.CurrentTags), nil
}

func (m *LogGroupManager) tagManagedSubscriptionFilters(ctx context.Context, logGroupName string, names []string) error {
	value, err := encodeManagedSubscriptionFilterNames(names)
	if err != nil {
		return fmt.Errorf("failed to encode managed subscription filter state: %w", err)
	}

	if err := m.client.TagLogGroup(ctx, logGroupName, map[string]string{
		managedSubscriptionFiltersTag: value,
	}); err != nil {
		return fmt.Errorf("failed to tag managed subscription filter state: %w", err)
	}

	return nil
}

func desiredSubscriptionFiltersByName(filters []SubscriptionFilterSpec) (map[string]SubscriptionFilterSpec, []string, error) {
	byName := make(map[string]SubscriptionFilterSpec, len(filters))
	names := make([]string, 0, len(filters))

	for _, filter := range filters {
		if filter.Name == "" {
			return nil, nil, fmt.Errorf("subscription filter name is required")
		}
		if filter.DestinationArn == "" {
			return nil, nil, fmt.Errorf("subscription filter %s destinationArn is required", filter.Name)
		}
		if _, exists := byName[filter.Name]; exists {
			return nil, nil, fmt.Errorf("subscription filter %s is defined more than once", filter.Name)
		}

		byName[filter.Name] = filter
		names = append(names, filter.Name)
	}

	sort.Strings(names)
	return byName, names, nil
}

func parseManagedSubscriptionFilterNames(tags map[string]string) map[string]struct{} {
	names := map[string]struct{}{}
	if len(tags) == 0 {
		return names
	}

	value := tags[managedSubscriptionFiltersTag]
	if value == "" {
		return names
	}

	var decoded []string
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		log.Warn("failed to parse managed subscription filter state tag", "error", err)
		return names
	}

	for _, name := range decoded {
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func encodeManagedSubscriptionFilterNames(names []string) (string, error) {
	sorted := append([]string{}, names...)
	sort.Strings(sorted)

	encoded, err := json.Marshal(sorted)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func userLogGroupTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}

	filtered := make(map[string]string, len(tags))
	for key, value := range tags {
		if key == managedSubscriptionFiltersTag {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func subscriptionFilterMatches(desired SubscriptionFilterSpec, current types.SubscriptionFilter) bool {
	if desired.DestinationArn != aws.ToString(current.DestinationArn) {
		return false
	}
	if desired.FilterPattern != aws.ToString(current.FilterPattern) {
		return false
	}
	if desired.RoleArn != aws.ToString(current.RoleArn) {
		return false
	}
	return subscriptionFilterDistributionMatches(desired.Distribution, string(current.Distribution))
}

func subscriptionFilterDistributionMatches(desired, current string) bool {
	if desired == "" {
		return current == "" || current == string(types.DistributionByLogStream)
	}
	return desired == current
}
