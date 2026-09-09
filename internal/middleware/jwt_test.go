package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildToken(t *testing.T) {
	token, err := BuildToken(1, "secret")
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestBuildToken_DifferentUsers(t *testing.T) {
	token1, err := BuildToken(1, "secret")
	require.NoError(t, err)
	token2, err := BuildToken(2, "secret")
	require.NoError(t, err)
	require.NotEqual(t, token1, token2)
}

func TestBuildToken_DifferentSecrets(t *testing.T) {
	token1, err := BuildToken(1, "secret1")
	require.NoError(t, err)
	token2, err := BuildToken(1, "secret2")
	require.NoError(t, err)
	require.NotEqual(t, token1, token2)
}

func TestValidateJWT_RoundTrip(t *testing.T) {
	secret := "my-secret"
	token, err := BuildToken(123, secret)
	require.NoError(t, err)

	userID, err := validateJWT(token, secret)
	require.NoError(t, err)
	require.Equal(t, int64(123), userID)
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	token, err := BuildToken(1, "secret1")
	require.NoError(t, err)

	_, err = validateJWT(token, "secret2")
	require.Error(t, err)
}
