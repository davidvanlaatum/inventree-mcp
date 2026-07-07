package oauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
)

const (
	tokenPrefix = "mcp1"
	nonceBytes  = 12
	keyBytes    = 32
)

type KeyState string

const (
	KeyStateActive      KeyState = "active"
	KeyStateDecryptOnly KeyState = "decrypt_only"
)

type Key struct {
	ID       string
	Material []byte
	State    KeyState
}

type Keyring struct {
	keys   map[string]Key
	active Key
}

func NewKeyring(keys []Key) (Keyring, error) {
	if len(keys) == 0 {
		return Keyring{}, errors.New("OAuth keyring requires at least one key")
	}
	keyMap := make(map[string]Key, len(keys))
	var active *Key
	for _, key := range keys {
		if key.ID == "" {
			return Keyring{}, errors.New("OAuth key ID is required")
		}
		if strings.ContainsAny(key.ID, ". \t\r\n") {
			return Keyring{}, fmt.Errorf("OAuth key ID %q contains unsupported characters", key.ID)
		}
		if len(key.Material) != keyBytes {
			return Keyring{}, fmt.Errorf("OAuth key %q must be %d bytes", key.ID, keyBytes)
		}
		if _, exists := keyMap[key.ID]; exists {
			return Keyring{}, fmt.Errorf("OAuth key ID %q is duplicated", key.ID)
		}
		switch key.State {
		case KeyStateActive:
			if active != nil {
				return Keyring{}, errors.New("OAuth keyring requires exactly one active key")
			}
			keyCopy := key
			active = &keyCopy
		case KeyStateDecryptOnly:
		default:
			return Keyring{}, fmt.Errorf("OAuth key %q has unsupported state %q", key.ID, key.State)
		}
		keyMap[key.ID] = key
	}
	if active == nil {
		return Keyring{}, errors.New("OAuth keyring requires exactly one active key")
	}
	return Keyring{keys: keyMap, active: *active}, nil
}

type EnvelopeCodec struct {
	Keyring Keyring
	Random  platform.RandomSource
}

func (c EnvelopeCodec) Seal(ctx context.Context, aad AssociatedData, claims any) (string, error) {
	key := c.Keyring.active
	aead, err := keyAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceBytes)
	random := c.Random
	if random == nil {
		random = platform.CryptoRandomSource{}
	}
	if err := random.ReadRandom(ctx, nonce); err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, associatedDataBytes(aad))
	payload := append(nonce, ciphertext...)
	return strings.Join([]string{
		tokenPrefix,
		key.ID,
		base64.RawURLEncoding.EncodeToString(payload),
	}, "."), nil
}

func (c EnvelopeCodec) Open(token string, aad AssociatedData, out any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return ErrInvalidToken
	}
	key, ok := c.Keyring.keys[parts[1]]
	if !ok {
		return ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(payload) <= nonceBytes {
		return ErrInvalidToken
	}
	aead, err := keyAEAD(key)
	if err != nil {
		return err
	}
	plaintext, err := aead.Open(nil, payload[:nonceBytes], payload[nonceBytes:], associatedDataBytes(aad))
	if err != nil {
		return ErrInvalidToken
	}
	if err := json.Unmarshal(plaintext, out); err != nil {
		return ErrInvalidToken
	}
	return nil
}

func keyAEAD(key Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key.Material)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func associatedDataBytes(aad AssociatedData) []byte {
	return []byte(aad.Issuer + "\x00" + aad.Audience + "\x00" + aad.ClientID + "\x00" + aad.Type)
}
