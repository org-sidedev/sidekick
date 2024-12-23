package secret_manager

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

type SecretManager interface {
	GetSecret(secretName string) (string, error)
	GetType() SecretManagerType
}

type SecretManagerType string

const (
	EnvSecretManagerType     SecretManagerType = "env"
	MockSecretManagerType    SecretManagerType = "mock"
	KeyringSecretManagerType SecretManagerType = "keyring"
)

type EnvSecretManager struct{}

func (e EnvSecretManager) GetSecret(secretName string) (string, error) {
	secretName = fmt.Sprintf("SIDE_%s", secretName)
	secret := os.Getenv(secretName)
	if secret == "" {
		return "", fmt.Errorf("secret %s not found in environment", secretName)
	}
	return secret, nil
}

func (e EnvSecretManager) GetType() SecretManagerType {
	return EnvSecretManagerType
}

type KeyringSecretManager struct{}

func (k KeyringSecretManager) GetSecret(secretName string) (string, error) {
	secret, err := keyring.Get("sidekick", secretName)
	if err != nil {
		return "", fmt.Errorf("error retrieving %s from keyring: %w", secretName, err)
	}
	return secret, nil
}

func (k KeyringSecretManager) GetType() SecretManagerType {
	return KeyringSecretManagerType
}

type MockSecretManager struct{}

func (e MockSecretManager) GetSecret(secretName string) (string, error) {
	return "fake secret", nil
}

func (e MockSecretManager) GetType() SecretManagerType {
	return MockSecretManagerType
}

type SecretManagerContainer struct {
	SecretManager
}

func (sc SecretManagerContainer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type    string
		Manager SecretManager
	}{
		Type:    string(sc.SecretManager.GetType()),
		Manager: sc.SecretManager,
	})
}

func (sc *SecretManagerContainer) UnmarshalJSON(data []byte) error {
	var v struct {
		Type    string
		Manager json.RawMessage
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	switch v.Type {
	case string(EnvSecretManagerType):
		var esm *EnvSecretManager
		if err := json.Unmarshal(v.Manager, &esm); err != nil {
			return err
		}
		sc.SecretManager = esm
	case string(MockSecretManagerType):
		var msm *MockSecretManager
		if err := json.Unmarshal(v.Manager, &msm); err != nil {
			return err
		}
		sc.SecretManager = msm
	case string(KeyringSecretManagerType):
		var ksm *KeyringSecretManager
		if err := json.Unmarshal(v.Manager, &ksm); err != nil {
			return err
		}
		sc.SecretManager = ksm
	default:
		return fmt.Errorf("unknown SecretManager type: %s", v.Type)
	}

	return nil
}
