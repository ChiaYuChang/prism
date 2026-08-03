package infra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulerToggleKey(t *testing.T) {
	require.Equal(t, "scheduler.toggle", SchedulerToggleGlobalKey)
	require.Equal(t, "scheduler.toggle.slow", SchedulerToggleKey("slow"))
}

func TestToggleValue(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		defaultVal bool
		want       bool
		wantErr    bool
	}{
		{name: "true", value: "true", want: true},
		{name: "false", value: "false", want: false},
		{name: "missing defaults true", value: nil, defaultVal: true, want: true},
		{name: "invalid", value: "pause", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toggleValue(tt.value, tt.defaultVal)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
