package utils

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
