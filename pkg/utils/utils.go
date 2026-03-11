package utils

// Ptr returns a pointer to the provided value v.
// Useful for handling optional database fields or API parameters that require pointers.
func Ptr[T any](v T) *T {
	return &v
}

// IfElse provides a generic ternary-like operation.
// Use with caution to maintain code readability.
func IfElse[T any](test bool, yes, no T) T {
	if test {
		return yes
	}
	return no
}

// DefaultIfZero returns the provided default value d if x is the zero value of its type.
func DefaultIfZero[T comparable](x, d T) T {
	var zero T
	if x == zero {
		return d
	}
	return x
}
