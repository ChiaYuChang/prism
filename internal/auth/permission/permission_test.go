package permission

import "testing"

func TestValidateForType(t *testing.T) {
	tests := []struct {
		name string
		mask Permission
		typ  string
		ok   bool
	}{
		{"full admin", DefaultAdmin, "admin", true},
		{"admin api only", AdminAPI, "admin", true},
		{"admin without admin api", TokenAdmin, "admin", false},
		{"user", UserAPI, "user", true},
		{"user escalation", AdminAPI, "user", false},
		{"root", RootBootstrap, "root", true},
		{"root with user bit", RootBootstrap | UserAPI, "root", false},
		{"reserved", Permission(0x02), "admin", false},
		{"worker historical", 0, "worker", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mask.ValidateForType(tt.typ) == nil; got != tt.ok {
				t.Fatalf("ValidateForType() = %v, want %v", got, tt.ok)
			}
		})
	}
}

func TestFromIntRejectsOutOfRange(t *testing.T) {
	for _, value := range []int16{-1, 256} {
		if _, err := FromInt(value); err == nil {
			t.Fatalf("FromInt(%d) returned nil error", value)
		}
	}
}
