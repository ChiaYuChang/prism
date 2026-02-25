package functional

// Predicate is a function that returns true if the element matches the condition.
type Predicate[T any] func(element T) bool

// Mapper is a function that transforms an element from type T to R.
type Mapper[T any, R any] func(element T) R

// Reducer defines how to combine an accumulator R and an element T.
type Reducer[T any, R any] func(accumulator R, element T) R

// Optional represents a value that may or may not be valid (Maybe Monad).
type Optional[T any] struct {
	isValid bool
	value   T
}

// Some creates a valid Optional with a value.
func Some[T any](v T) Optional[T] {
	return Optional[T]{isValid: true, value: v}
}

// None creates an empty Optional.
func None[T any]() Optional[T] {
	return Optional[T]{isValid: false}
}

// IsValid returns true if the Optional contains a value.
func (o Optional[T]) IsValid() bool { return o.isValid }

// Get returns the value or panics if invalid.
func (o Optional[T]) Get() T {
	if !o.isValid {
		panic("accessing invalid optional value")
	}
	return o.value
}

// OrElse returns the value if valid, otherwise returns the default value.
func (o Optional[T]) OrElse(defaultVal T) T {
	if o.isValid {
		return o.value
	}
	return defaultVal
}

// Filter returns a new slice containing only elements that satisfy the predicate.
func Filter[T any](collection []T, p Predicate[T]) []T {
	if len(collection) == 0 {
		return nil
	}

	result := make([]T, 0, len(collection))
	for _, item := range collection {
		if p(item) {
			result = append(result, item)
		}
	}
	return result
}

// Map applies the mapper function to each element and returns a slice of the results.
func Map[T any, R any](collection []T, m Mapper[T, R]) []R {
	if len(collection) == 0 {
		return nil
	}

	result := make([]R, len(collection))
	for i, item := range collection {
		result[i] = m(item)
	}
	return result
}

// Reduce collapses a slice into a single value.
func Reduce[T any, R any](collection []T, initial R, r Reducer[T, R]) R {
	accumulator := initial
	for _, item := range collection {
		accumulator = r(accumulator, item)
	}
	return accumulator
}

// FlatMap maps each element to a slice and flattens the result into a single slice.
func FlatMap[T any, R any](collection []T, f func(T) []R) []R {
	if len(collection) == 0 {
		return nil
	}

	var result []R
	for _, item := range collection {
		result = append(result, f(item)...)
	}
	return result
}
