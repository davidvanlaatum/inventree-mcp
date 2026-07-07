package oauth

import (
	"encoding/base64"
	"errors"
	"fmt"
)

type KeyConfig struct {
	ID             string
	MaterialBase64 string
	State          KeyState
}

type KeyringConfig struct {
	Keys []KeyConfig
}

func (c KeyringConfig) Keyring() (Keyring, error) {
	keys := make([]Key, 0, len(c.Keys))
	for _, configured := range c.Keys {
		material, err := decodeKeyMaterial(configured.MaterialBase64)
		if err != nil {
			return Keyring{}, fmt.Errorf("OAuth key %q: %w", configured.ID, err)
		}
		keys = append(keys, Key{
			ID:       configured.ID,
			Material: material,
			State:    configured.State,
		})
	}
	return NewKeyring(keys)
}

func decodeKeyMaterial(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("key material is required")
	}
	if material, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		return material, nil
	}
	if material, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return material, nil
	}
	if material, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return material, nil
	}
	if material, err := base64.URLEncoding.DecodeString(raw); err == nil {
		return material, nil
	}
	return nil, errors.New("key material must be base64 encoded")
}
