// Package permission defines the bitmask used to authorize Prism tokens.
package permission

import "fmt"

// Permission is an authorization bitmask persisted as a small integer.
type Permission uint8

const (
	UserAPI       Permission = 0x01
	AdminAPI      Permission = 0x20
	TokenAdmin    Permission = 0x40
	RootBootstrap Permission = 0x80

	reserved Permission = 0x1e
	adminMax Permission = AdminAPI | TokenAdmin | UserAPI
)

// DefaultAdmin is the full routine administrator mask.
const DefaultAdmin Permission = AdminAPI | TokenAdmin | UserAPI

// DefaultUser is the user API mask.
const DefaultUser Permission = UserAPI

// Root is the root-only mask.
const Root Permission = RootBootstrap

// Has reports whether p includes every bit in required.
func (p Permission) Has(required Permission) bool {
	return p&required == required
}

// IsSubset reports whether p grants no permission outside parent.
func (p Permission) IsSubset(parent Permission) bool {
	return p&^parent == 0
}

// FromInt converts a persisted integer without truncating invalid values.
func FromInt(value int16) (Permission, error) {
	if value < 0 || value > 255 {
		return 0, fmt.Errorf("permission value %d is outside 0..255", value)
	}
	return Permission(value), nil
}

// Validate checks the permission mask independently of token type.
func (p Permission) Validate() error {
	if p&reserved != 0 {
		return fmt.Errorf("permission mask 0x%02x contains reserved bits", uint8(p))
	}
	if p&RootBootstrap != 0 && p != RootBootstrap {
		return fmt.Errorf("root permission must be exactly 0x%02x", uint8(RootBootstrap))
	}
	return nil
}

// ValidateForType checks the mask allowed for a token type.
func (p Permission) ValidateForType(tokenType string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	switch tokenType {
	case "root":
		if p != RootBootstrap {
			return fmt.Errorf("root permission must be exactly 0x%02x", uint8(RootBootstrap))
		}
	case "admin":
		if p == 0 || !p.Has(AdminAPI) || !p.IsSubset(adminMax) {
			return fmt.Errorf("invalid admin permission mask 0x%02x", uint8(p))
		}
	case "user":
		if p != UserAPI {
			return fmt.Errorf("user permission must be exactly 0x%02x", uint8(UserAPI))
		}
	case "worker":
		if p != 0 {
			return fmt.Errorf("worker permission must be zero")
		}
	default:
		return fmt.Errorf("unsupported token type %q", tokenType)
	}
	return nil
}
