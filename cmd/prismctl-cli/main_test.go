package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandIsAPIOnly(t *testing.T) {
	cmd := newRootCommand()

	_, _, err := cmd.Find([]string{"root"})
	require.Error(t, err)
	_, _, err = cmd.Find([]string{"admin", "tokens", "list"})
	require.NoError(t, err)
	require.Nil(t, cmd.PersistentFlags().Lookup("pg-host"))
	require.Nil(t, cmd.PersistentFlags().Lookup("root-token-file"))
}
