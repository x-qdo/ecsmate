package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/secrets"
)

var (
	kmsKeyArn   string
	secretValue string
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage encrypted secrets",
	Long: `Manage encrypted secrets files using envelope encryption with AWS KMS.

Secrets are encrypted using AES-256-GCM with a data key that is itself
encrypted by AWS KMS. This provides secure at-rest encryption while
allowing the secrets to be stored in version control.`,
}

var secretsEncryptCmd = &cobra.Command{
	Use:   "encrypt <plaintext-file>",
	Short: "Encrypt a plaintext YAML file",
	Long: `Encrypt a plaintext YAML file containing secrets.

The input file should be a YAML file with key-value pairs:

  db_password: mysecretpassword
  api_key: myapikey

The output will be written to <file>.enc.yaml with envelope encryption.`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretsEncrypt,
}

var secretsDecryptCmd = &cobra.Command{
	Use:   "decrypt <encrypted-file>",
	Short: "Decrypt an encrypted secrets file to stdout",
	Long: `Decrypt an encrypted secrets file and output the plaintext to stdout.

The decrypted content is never written to disk.`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretsDecrypt,
}

var secretsSetCmd = &cobra.Command{
	Use:   "set <encrypted-file> <key>",
	Short: "Set or update a secret value",
	Long: `Set or update a secret in an encrypted file.

If --value is not provided, the value will be read from stdin.`,
	Args: cobra.ExactArgs(2),
	RunE: runSecretsSet,
}

var secretsDeleteCmd = &cobra.Command{
	Use:   "delete <encrypted-file> <key>",
	Short: "Delete a secret from an encrypted file",
	Args:  cobra.ExactArgs(2),
	RunE:  runSecretsDelete,
}

func init() {
	secretsEncryptCmd.Flags().StringVar(&kmsKeyArn, "kms-arn", "", "KMS key ARN for encryption (required)")
	secretsEncryptCmd.MarkFlagRequired("kms-arn")

	secretsSetCmd.Flags().StringVar(&secretValue, "value", "", "Secret value (reads from stdin if not provided)")

	secretsCmd.AddCommand(secretsEncryptCmd)
	secretsCmd.AddCommand(secretsDecryptCmd)
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsDeleteCmd)
}

func runSecretsEncrypt(cmd *cobra.Command, args []string) error {
	inputFile := args[0]
	ctx := cmd.Context()

	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	log.Debug("encrypting secrets file", "input", inputFile, "kmsArn", kmsKeyArn)

	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	var plainData map[string]string
	if err := yaml.Unmarshal(data, &plainData); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(plainData) == 0 {
		return fmt.Errorf("input file contains no key-value pairs")
	}

	kmsClient, err := aws.NewKMSClient(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to create KMS client: %w", err)
	}

	envelope := secrets.NewEnvelope(kmsClient)
	ef, err := envelope.EncryptFile(ctx, kmsKeyArn, plainData)
	if err != nil {
		return err
	}

	outputFile := strings.TrimSuffix(inputFile, ".yaml") + ".enc.yaml"
	if strings.HasSuffix(inputFile, ".yml") {
		outputFile = strings.TrimSuffix(inputFile, ".yml") + ".enc.yaml"
	}

	if err := ef.Save(outputFile); err != nil {
		return err
	}

	fmt.Printf("%s Encrypted %d secrets to %s\n", green("✓"), len(plainData), cyan(outputFile))
	fmt.Printf("\nRemember to delete the plaintext file: %s\n", inputFile)

	return nil
}

func runSecretsDecrypt(cmd *cobra.Command, args []string) error {
	inputFile := args[0]
	ctx := cmd.Context()

	log.Debug("decrypting secrets file", "input", inputFile)

	ef, err := secrets.LoadEncryptedFile(inputFile)
	if err != nil {
		return err
	}

	kmsClient, err := aws.NewKMSClient(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to create KMS client: %w", err)
	}

	envelope := secrets.NewEnvelope(kmsClient)
	plainData, err := envelope.DecryptFile(ctx, ef)
	if err != nil {
		return err
	}

	output, err := yaml.Marshal(plainData)
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	fmt.Print(string(output))
	return nil
}

func runSecretsSet(cmd *cobra.Command, args []string) error {
	inputFile := args[0]
	key := args[1]
	ctx := cmd.Context()

	green := color.New(color.FgGreen).SprintFunc()

	value := secretValue
	if value == "" {
		fmt.Fprintf(os.Stderr, "Enter value for %s: ", key)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read value: %w", err)
		}
		value = strings.TrimSpace(line)
	}

	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}

	log.Debug("setting secret", "file", inputFile, "key", key)

	ef, err := secrets.LoadEncryptedFile(inputFile)
	if err != nil {
		return err
	}

	kmsClient, err := aws.NewKMSClient(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to create KMS client: %w", err)
	}

	envelope := secrets.NewEnvelope(kmsClient)
	if err := envelope.EncryptValue(ctx, ef, key, value); err != nil {
		return err
	}

	if err := ef.Save(inputFile); err != nil {
		return err
	}

	fmt.Printf("%s Set secret %s\n", green("✓"), key)
	return nil
}

func runSecretsDelete(cmd *cobra.Command, args []string) error {
	inputFile := args[0]
	key := args[1]
	ctx := cmd.Context()

	green := color.New(color.FgGreen).SprintFunc()

	log.Debug("deleting secret", "file", inputFile, "key", key)

	ef, err := secrets.LoadEncryptedFile(inputFile)
	if err != nil {
		return err
	}

	kmsClient, err := aws.NewKMSClient(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to create KMS client: %w", err)
	}

	envelope := secrets.NewEnvelope(kmsClient)
	if err := envelope.DeleteValue(ef, key); err != nil {
		return err
	}

	if err := envelope.UpdateMAC(ctx, ef); err != nil {
		return err
	}

	if err := ef.Save(inputFile); err != nil {
		return err
	}

	fmt.Printf("%s Deleted secret %s\n", green("✓"), key)
	return nil
}
