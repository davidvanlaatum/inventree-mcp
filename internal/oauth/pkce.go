package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

func PKCEChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func verifyPKCES256(verifier string, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	return PKCEChallengeS256(verifier) == challenge
}
