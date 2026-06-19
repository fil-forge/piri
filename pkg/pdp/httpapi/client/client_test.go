package client

import (
	"strings"
	"testing"

	"github.com/fil-forge/ucantone/testutil"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestCreateAuthBearerTokenFromID(t *testing.T) {
	signer := testutil.RandomMultikeyIssuer(t)

	token, err := createAuthBearerTokenFromID(signer)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(token, "Bearer "))

	parsed, err := jwt.Parse(strings.TrimPrefix(token, "Bearer "), func(token *jwt.Token) (interface{}, error) {
		require.Equal(t, jwt.SigningMethodEdDSA.Alg(), token.Method.Alg())
		return signer.PublicKey(), nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)

	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, "storacha", claims["service_name"])
}

func TestWithBearerFromSignerSetsHeader(t *testing.T) {
	signer := testutil.RandomMultikeySigner(t)
	client := &Client{}

	err := WithBearerFromSigner(signer)(client)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(client.authHeader, "Bearer "))
}
