package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandProvidesBreakGlassOperations(t *testing.T) {
	cmd := newRootCommand()

	for _, args := range [][]string{{"init"}, {"check"}, {"admin-create"}, {"tokens-revoke-all"}} {
		_, _, err := cmd.Find(args)
		require.NoError(t, err, args)
	}
	require.NotNil(t, cmd.PersistentFlags().Lookup("pg-host"))
	require.NotNil(t, cmd.PersistentFlags().Lookup("root-token-file"))
}
