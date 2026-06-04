package resources

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/x-qdo/ecsmate/internal/config"
)

type mockSSMClient struct {
	params    map[string]string
	tags      map[string][]types.Tag
	putCalls  []string
	putKeyIDs map[string]string
	delCalls  []string
	failGet   map[string]bool
	failPut   bool
	failTags  bool
}

func newMockSSMClient() *mockSSMClient {
	return &mockSSMClient{
		params:    make(map[string]string),
		tags:      make(map[string][]types.Tag),
		putKeyIDs: make(map[string]string),
		failGet:   make(map[string]bool),
	}
}

func (m *mockSSMClient) GetParameter(ctx context.Context, input *ssm.GetParameterInput, opts ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	name := aws.ToString(input.Name)
	if m.failGet[name] {
		return nil, fmt.Errorf("parameter not found: %s", name)
	}
	val, ok := m.params[name]
	if !ok {
		return nil, fmt.Errorf("parameter not found: %s", name)
	}
	return &ssm.GetParameterOutput{
		Parameter: &types.Parameter{
			Name:  input.Name,
			Value: aws.String(val),
		},
	}, nil
}

func (m *mockSSMClient) PutParameter(ctx context.Context, input *ssm.PutParameterInput, opts ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	if m.failPut {
		return nil, fmt.Errorf("PutParameter failed")
	}
	name := aws.ToString(input.Name)
	m.params[name] = aws.ToString(input.Value)
	m.putKeyIDs[name] = aws.ToString(input.KeyId)
	m.putCalls = append(m.putCalls, name)
	return &ssm.PutParameterOutput{Version: 1}, nil
}

func (m *mockSSMClient) DeleteParameter(ctx context.Context, input *ssm.DeleteParameterInput, opts ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	name := aws.ToString(input.Name)
	delete(m.params, name)
	m.delCalls = append(m.delCalls, name)
	return &ssm.DeleteParameterOutput{}, nil
}

func (m *mockSSMClient) ListTagsForResource(ctx context.Context, input *ssm.ListTagsForResourceInput, opts ...func(*ssm.Options)) (*ssm.ListTagsForResourceOutput, error) {
	if m.failTags {
		return nil, fmt.Errorf("ListTagsForResource failed")
	}
	name := aws.ToString(input.ResourceId)
	return &ssm.ListTagsForResourceOutput{
		TagList: m.tags[name],
	}, nil
}

func (m *mockSSMClient) AddTagsToResource(ctx context.Context, input *ssm.AddTagsToResourceInput, opts ...func(*ssm.Options)) (*ssm.AddTagsToResourceOutput, error) {
	name := aws.ToString(input.ResourceId)
	m.tags[name] = input.Tags
	return &ssm.AddTagsToResourceOutput{}, nil
}

func (m *mockSSMClient) DescribeParameters(ctx context.Context, input *ssm.DescribeParametersInput, opts ...func(*ssm.Options)) (*ssm.DescribeParametersOutput, error) {
	var params []types.ParameterMetadata
	for name := range m.params {
		if tags, ok := m.tags[name]; ok {
			for _, tag := range tags {
				if aws.ToString(tag.Key) == "ManagedBy" && aws.ToString(tag.Value) == "ecsmate" {
					params = append(params, types.ParameterMetadata{Name: aws.String(name)})
					break
				}
			}
		}
	}
	return &ssm.DescribeParametersOutput{Parameters: params}, nil
}

func (m *mockSSMClient) setManagedTag(name string) {
	m.tags[name] = []types.Tag{{Key: aws.String("ManagedBy"), Value: aws.String("ecsmate")}}
}

func TestSSMParamsManager_NilManaged(t *testing.T) {
	mgr := NewSSMParamsManager(nil, nil)
	if mgr != nil {
		t.Error("expected nil manager when managed secrets is nil")
	}
}

func TestSSMParamsManager_Diff_CreateNew(t *testing.T) {
	mock := newMockSSMClient()
	mock.failGet["/myapp/db_password"] = true

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789",
	}

	mgr := &SSMParamsManager{
		client:  nil,
		managed: managed,
	}

	// Create a wrapper that uses our mock
	changes, err := diffWithMock(mgr, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Action != "create" {
		t.Errorf("expected action 'create', got '%s'", changes[0].Action)
	}

	if changes[0].Name != "/myapp/db_password" {
		t.Errorf("expected name '/myapp/db_password', got '%s'", changes[0].Name)
	}

	if changes[0].NewHash == "" {
		t.Error("expected NewHash to be set")
	}
}

func TestSSMParamsManager_Diff_UpdateExisting(t *testing.T) {
	mock := newMockSSMClient()
	mock.params["/myapp/db_password"] = "oldsecret"
	mock.setManagedTag("/myapp/db_password")

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "newsecret",
		},
		SSMPrefix: "/myapp",
	}

	mgr := &SSMParamsManager{managed: managed}

	changes, err := diffWithMock(mgr, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	if changes[0].Action != "update" {
		t.Errorf("expected action 'update', got '%s'", changes[0].Action)
	}

	if changes[0].OldHash == "" {
		t.Error("expected OldHash to be set for update")
	}

	if changes[0].NewHash == "" {
		t.Error("expected NewHash to be set for update")
	}

	if changes[0].OldHash == changes[0].NewHash {
		t.Error("OldHash and NewHash should differ for update")
	}
}

func TestSSMParamsManager_Diff_NoChange(t *testing.T) {
	mock := newMockSSMClient()
	secret := "samesecret"
	mock.params["/myapp/db_password"] = secret
	mock.setManagedTag("/myapp/db_password")

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": secret,
		},
		SSMPrefix: "/myapp",
	}

	mgr := &SSMParamsManager{managed: managed}

	changes, err := diffWithMock(mgr, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 0 {
		t.Errorf("expected 0 changes when value unchanged, got %d", len(changes))
	}
}

func TestSSMParamsManager_Diff_OrphanDetection(t *testing.T) {
	mock := newMockSSMClient()
	mock.params["/myapp/db_password"] = "current"
	mock.params["/myapp/old_key"] = "orphaned"
	mock.setManagedTag("/myapp/db_password")
	mock.setManagedTag("/myapp/old_key")

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "current",
		},
		SSMPrefix: "/myapp",
	}

	mgr := &SSMParamsManager{managed: managed}

	changes, err := diffWithMock(mgr, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var deleteChange *SSMParamChange
	for i := range changes {
		if changes[i].Action == "delete" {
			deleteChange = &changes[i]
			break
		}
	}

	if deleteChange == nil {
		t.Fatal("expected delete action for orphaned parameter")
	}

	if deleteChange.Name != "/myapp/old_key" {
		t.Errorf("expected orphan name '/myapp/old_key', got '%s'", deleteChange.Name)
	}
}

func TestSSMParamsManager_Diff_NotManagedByUs(t *testing.T) {
	mock := newMockSSMClient()
	mock.params["/myapp/external_secret"] = "someone_elses"
	// No ManagedBy tag

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"external_secret": "newvalue",
		},
		SSMPrefix: "/myapp",
	}

	mgr := &SSMParamsManager{managed: managed}

	_, err := diffWithMock(mgr, mock)
	if err == nil {
		t.Fatal("expected error when trying to manage unowned parameter")
	}
}

func TestSSMParamsManager_Diff_MultipleSecrets(t *testing.T) {
	mock := newMockSSMClient()
	mock.failGet["/myapp/new_secret"] = true
	mock.params["/myapp/existing"] = "oldvalue"
	mock.setManagedTag("/myapp/existing")

	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"new_secret": "value1",
			"existing":   "newvalue",
		},
		SSMPrefix: "/myapp",
	}

	mgr := &SSMParamsManager{managed: managed}

	changes, err := diffWithMock(mgr, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	actions := make(map[string]bool)
	for _, c := range changes {
		actions[c.Action] = true
	}

	if !actions["create"] {
		t.Error("expected a create action")
	}
	if !actions["update"] {
		t.Error("expected an update action")
	}
}

func TestSSMParamsManager_ApplyUsesSSMKMSKeyID(t *testing.T) {
	mock := newMockSSMClient()
	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix:    "/myapp",
		SSMKMSKeyID:  "alias/app-ssm",
		KMSKeyArn:    "arn:aws:kms:eu-west-1:123456789012:key/source",
		KMSKeyRegion: "eu-west-1",
		Region:       "us-east-2",
	}

	mgr := NewSSMParamsManager(mock, managed)
	if err := mgr.Apply(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := mock.putKeyIDs["/myapp/db_password"]; got != "alias/app-ssm" {
		t.Fatalf("expected PutParameter KeyId alias/app-ssm, got %q", got)
	}
}

func TestSSMParamsManager_ApplyOmitsKeyIDWhenUnset(t *testing.T) {
	mock := newMockSSMClient()
	managed := &config.ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix:    "/myapp",
		KMSKeyArn:    "arn:aws:kms:eu-west-1:123456789012:key/source",
		KMSKeyRegion: "eu-west-1",
		Region:       "us-east-2",
	}

	mgr := NewSSMParamsManager(mock, managed)
	if err := mgr.Apply(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := mock.putKeyIDs["/myapp/db_password"]; got != "" {
		t.Fatalf("expected PutParameter KeyId to be unset, got %q", got)
	}
}

func TestShortHash(t *testing.T) {
	hash1 := shortHash("secret1")
	hash2 := shortHash("secret2")

	if hash1 == hash2 {
		t.Error("different values should produce different hashes")
	}

	if len(hash1) != 8 {
		t.Errorf("expected hash length 8, got %d", len(hash1))
	}

	// Same input should produce same hash
	hash1Again := shortHash("secret1")
	if hash1 != hash1Again {
		t.Error("same value should produce same hash")
	}
}

func TestSSMParamChange_Fields(t *testing.T) {
	change := SSMParamChange{
		Name:    "/app/secret",
		Action:  "create",
		OldHash: "abc12345",
		NewHash: "def67890",
	}

	if change.Name != "/app/secret" {
		t.Errorf("unexpected Name: %s", change.Name)
	}
	if change.Action != "create" {
		t.Errorf("unexpected Action: %s", change.Action)
	}
	if change.OldHash != "abc12345" {
		t.Errorf("unexpected OldHash: %s", change.OldHash)
	}
	if change.NewHash != "def67890" {
		t.Errorf("unexpected NewHash: %s", change.NewHash)
	}
}

// diffWithMock is a helper that uses a mock client for testing
func diffWithMock(mgr *SSMParamsManager, mock *mockSSMClient) ([]SSMParamChange, error) {
	if mgr == nil || mgr.managed == nil {
		return nil, nil
	}

	ctx := context.Background()
	var changes []SSMParamChange

	for key, newValue := range mgr.managed.Decrypted {
		paramName := fmt.Sprintf("%s/%s", mgr.managed.SSMPrefix, key)
		newHash := shortHash(newValue)

		existing, err := mock.GetParameter(ctx, &ssm.GetParameterInput{
			Name:           aws.String(paramName),
			WithDecryption: aws.Bool(true),
		})

		if err != nil {
			changes = append(changes, SSMParamChange{
				Name:    paramName,
				Action:  "create",
				NewHash: newHash,
			})
			continue
		}

		tags, err := mock.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
			ResourceType: types.ResourceTypeForTaggingParameter,
			ResourceId:   aws.String(paramName),
		})
		if err != nil {
			return nil, err
		}

		isManagedByUs := false
		for _, tag := range tags.TagList {
			if aws.ToString(tag.Key) == "ManagedBy" && aws.ToString(tag.Value) == "ecsmate" {
				isManagedByUs = true
				break
			}
		}
		if !isManagedByUs {
			return nil, fmt.Errorf("SSM parameter %s exists but not managed by ecsmate (missing ManagedBy=ecsmate tag)", paramName)
		}

		oldHash := shortHash(aws.ToString(existing.Parameter.Value))
		if oldHash != newHash {
			changes = append(changes, SSMParamChange{
				Name:    paramName,
				Action:  "update",
				OldHash: oldHash,
				NewHash: newHash,
			})
		}
	}

	// Find orphans
	params, _ := mock.DescribeParameters(ctx, &ssm.DescribeParametersInput{})
	prefix := mgr.managed.SSMPrefix + "/"
	for _, p := range params.Parameters {
		name := aws.ToString(p.Name)
		if len(name) > len(prefix) {
			key := name[len(prefix):]
			if _, exists := mgr.managed.Decrypted[key]; !exists {
				changes = append(changes, SSMParamChange{
					Name:   name,
					Action: "delete",
				})
			}
		}
	}

	return changes, nil
}
