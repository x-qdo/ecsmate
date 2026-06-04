package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

type SSMParamChange struct {
	Name    string
	Action  string
	OldHash string
	NewHash string
}

type ssmParameterClient interface {
	GetParameter(context.Context, *ssm.GetParameterInput, ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(context.Context, *ssm.PutParameterInput, ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	DeleteParameter(context.Context, *ssm.DeleteParameterInput, ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
	ListTagsForResource(context.Context, *ssm.ListTagsForResourceInput, ...func(*ssm.Options)) (*ssm.ListTagsForResourceOutput, error)
	AddTagsToResource(context.Context, *ssm.AddTagsToResourceInput, ...func(*ssm.Options)) (*ssm.AddTagsToResourceOutput, error)
	DescribeParameters(context.Context, *ssm.DescribeParametersInput, ...func(*ssm.Options)) (*ssm.DescribeParametersOutput, error)
}

type SSMParamsManager struct {
	client      ssmParameterClient
	managed     *config.ManagedSecrets
	ssmKMSKeyID string
}

func NewSSMParamsManager(client ssmParameterClient, managed *config.ManagedSecrets) *SSMParamsManager {
	if managed == nil {
		return nil
	}
	return &SSMParamsManager{
		client:      client,
		managed:     managed,
		ssmKMSKeyID: managed.SSMKMSKeyID,
	}
}

func shortHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])[:8]
}

func (m *SSMParamsManager) Diff(ctx context.Context) ([]SSMParamChange, error) {
	if m == nil || m.managed == nil {
		return nil, nil
	}

	var changes []SSMParamChange

	keys := make([]string, 0, len(m.managed.Decrypted))
	for k := range m.managed.Decrypted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		newValue := m.managed.Decrypted[key]
		paramName := fmt.Sprintf("%s/%s", m.managed.SSMPrefix, key)
		newHash := shortHash(newValue)

		existing, err := m.client.GetParameter(ctx, &ssm.GetParameterInput{
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

		isManagedByUs, err := m.checkOwnership(ctx, paramName)
		if err != nil {
			return nil, err
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

	orphans, err := m.findOrphans(ctx)
	if err != nil {
		return nil, err
	}
	for _, name := range orphans {
		changes = append(changes, SSMParamChange{
			Name:   name,
			Action: "delete",
		})
	}

	return changes, nil
}

func (m *SSMParamsManager) Apply(ctx context.Context) error {
	if m == nil || m.managed == nil {
		return nil
	}

	keys := make([]string, 0, len(m.managed.Decrypted))
	for k := range m.managed.Decrypted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := m.managed.Decrypted[key]
		paramName := fmt.Sprintf("%s/%s", m.managed.SSMPrefix, key)
		newHash := shortHash(value)

		existing, err := m.client.GetParameter(ctx, &ssm.GetParameterInput{
			Name:           aws.String(paramName),
			WithDecryption: aws.Bool(true),
		})

		action := "Creating"
		logMsg := fmt.Sprintf("Creating SSM parameter %s (hash: %s)", paramName, newHash)

		if err == nil {
			isManagedByUs, err := m.checkOwnership(ctx, paramName)
			if err != nil {
				return err
			}
			if !isManagedByUs {
				return fmt.Errorf("SSM parameter %s exists but not managed by ecsmate", paramName)
			}

			oldHash := shortHash(aws.ToString(existing.Parameter.Value))
			if oldHash == newHash {
				log.Debug("SSM parameter unchanged", "name", paramName, "hash", newHash)
				continue
			}
			action = "Updating"
			logMsg = fmt.Sprintf("Updating SSM parameter %s (hash: %s → %s)", paramName, oldHash, newHash)
		}

		log.Info(logMsg)

		input := &ssm.PutParameterInput{
			Name:      aws.String(paramName),
			Value:     aws.String(value),
			Type:      types.ParameterTypeSecureString,
			Overwrite: aws.Bool(true),
		}

		if m.ssmKMSKeyID != "" {
			input.KeyId = aws.String(m.ssmKMSKeyID)
		}

		_, err = m.client.PutParameter(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to %s SSM parameter %s: %w", action, paramName, err)
		}

		_, err = m.client.AddTagsToResource(ctx, &ssm.AddTagsToResourceInput{
			ResourceType: types.ResourceTypeForTaggingParameter,
			ResourceId:   aws.String(paramName),
			Tags: []types.Tag{
				{Key: aws.String("ManagedBy"), Value: aws.String("ecsmate")},
			},
		})
		if err != nil {
			log.Warn("failed to tag SSM parameter", "name", paramName, "error", err)
		}
	}

	orphans, err := m.findOrphans(ctx)
	if err != nil {
		return err
	}
	for _, name := range orphans {
		log.Info(fmt.Sprintf("Deleting orphaned SSM parameter %s", name))
		_, err := m.client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
			Name: aws.String(name),
		})
		if err != nil {
			return fmt.Errorf("failed to delete orphaned SSM parameter %s: %w", name, err)
		}
	}

	return nil
}

func (m *SSMParamsManager) checkOwnership(ctx context.Context, paramName string) (bool, error) {
	tags, err := m.client.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
		ResourceType: types.ResourceTypeForTaggingParameter,
		ResourceId:   aws.String(paramName),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list tags for %s: %w", paramName, err)
	}

	for _, tag := range tags.TagList {
		if aws.ToString(tag.Key) == "ManagedBy" && aws.ToString(tag.Value) == "ecsmate" {
			return true, nil
		}
	}
	return false, nil
}

func (m *SSMParamsManager) findOrphans(ctx context.Context) ([]string, error) {
	prefix := m.managed.SSMPrefix + "/"

	params, err := m.client.DescribeParameters(ctx, &ssm.DescribeParametersInput{
		ParameterFilters: []types.ParameterStringFilter{
			{
				Key:    aws.String("Name"),
				Option: aws.String("BeginsWith"),
				Values: []string{prefix},
			},
			{
				Key:    aws.String("tag:ManagedBy"),
				Values: []string{"ecsmate"},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe parameters: %w", err)
	}

	var orphans []string
	for _, p := range params.Parameters {
		name := aws.ToString(p.Name)
		key := name[len(prefix):]

		if _, exists := m.managed.Decrypted[key]; !exists {
			orphans = append(orphans, name)
		}
	}

	return orphans, nil
}

func (m *SSMParamsManager) GetChanges() []SSMParamChange {
	return nil
}
