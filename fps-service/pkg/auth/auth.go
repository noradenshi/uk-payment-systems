package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

type ContextKey string

const BICKey ContextKey = "auth_bic"

var ErrInvalidAPIKey = errors.New("invalid API key")

func GenerateAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func BICFromContext(ctx context.Context) string {
	bic, _ := ctx.Value(BICKey).(string)
	return bic
}
