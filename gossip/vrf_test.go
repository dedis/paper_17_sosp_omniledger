package gossip

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/dedis/crypto.v0/config"
	"gopkg.in/dedis/crypto.v0/ed25519"
)

var suite = ed25519.NewAES128SHA256Ed25519(false)

func TestVRF(t *testing.T) {
	kp := config.NewKeyPair(suite)
	msg := []byte("HelloWorld")
	sum, proof, err := Hash(suite, kp.Secret, msg)
	require.Nil(t, err)
	assert.False(t, len(sum) == 0)

	assert.True(t, Verify(suite, kp.Public, msg, sum, proof))
}
