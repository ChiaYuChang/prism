package tree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBuildsChildren(t *testing.T) {
	tree := New[bool]()
	require.NoError(t, tree.Add("fast", "global", true))
	require.NoError(t, tree.Add("global", "", true))
	require.NoError(t, tree.Validate())

	global, ok := tree.Node("global")
	require.True(t, ok)
	_, ok = global.Children["fast"]
	require.True(t, ok)
}

func TestValidateRejectsInvalidRelationships(t *testing.T) {
	tests := []struct {
		name string
		make func(*Tree[bool])
		want error
	}{
		{
			name: "missing parent",
			make: func(tree *Tree[bool]) {
				require.NoError(t, tree.Add("fast", "global", true))
			},
			want: ErrMissingParent,
		},
		{
			name: "cycle",
			make: func(tree *Tree[bool]) {
				require.NoError(t, tree.Add("fast", "slow", true))
				require.NoError(t, tree.Add("slow", "fast", true))
			},
			want: ErrCycle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := New[bool]()
			tt.make(tree)
			require.ErrorIs(t, tree.Validate(), tt.want)
		})
	}
}

func TestAddRejectsDuplicate(t *testing.T) {
	tree := New[bool]()
	require.NoError(t, tree.Add("global", "", true))
	require.ErrorIs(t, tree.Add("global", "", true), ErrDuplicateNode)
}

func TestEffective(t *testing.T) {
	tree := New[bool]()
	require.NoError(t, tree.Add("global", "", false))
	require.NoError(t, tree.Add("fast", "global", true))

	effective, err := tree.Effective("fast", func(enabled bool) bool { return enabled })
	require.NoError(t, err)
	require.False(t, effective)

	global, ok := tree.Node("global")
	require.True(t, ok)
	global.Payload = true
	effective, err = tree.Effective("fast", func(enabled bool) bool { return enabled })
	require.NoError(t, err)
	require.True(t, effective)
}

func TestEffectiveRejectsMissingNode(t *testing.T) {
	tree := New[bool]()
	_, err := tree.Effective("missing", func(enabled bool) bool { return enabled })
	require.Error(t, err)
}
