package utils

// Ptr returns a pointer to the provided value v.
// Useful for handling optional database fields or API parameters that require pointers.
func Ptr[T any](v T) *T {
	return &v
}
