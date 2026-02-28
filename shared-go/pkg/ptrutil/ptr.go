// Package ptrutil provides generic pointer utility functions.
package ptrutil

// Ptr returns a pointer to the given value.
// This is a generic version that works with any type.
//
//go:fix inline
func Ptr[T any](v T) *T {
	return new(v)
}

// String returns a pointer to the given string.
// Convenience alias for Ptr[string].
//
//go:fix inline
func String(s string) *string {
	return new(s)
}

// Int returns a pointer to the given int.
//
//go:fix inline
func Int(i int) *int {
	return new(i)
}

// Int64 returns a pointer to the given int64.
//
//go:fix inline
func Int64(i int64) *int64 {
	return new(i)
}

// Bool returns a pointer to the given bool.
//
//go:fix inline
func Bool(b bool) *bool {
	return new(b)
}

// Float64 returns a pointer to the given float64.
//
//go:fix inline
func Float64(f float64) *float64 {
	return new(f)
}

// Deref returns the value pointed to by ptr, or zero value if ptr is nil.
func Deref[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}

// DerefOr returns the value pointed to by ptr, or defaultVal if ptr is nil.
func DerefOr[T any](ptr *T, defaultVal T) T {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}
