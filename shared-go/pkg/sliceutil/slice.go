// Package sliceutil provides generic utility functions for slice operations.
//
// This package offers commonly-used slice manipulation functions that work
// with any comparable type through Go generics (1.18+). All functions preserve
// the original slice and return new slices to ensure immutability.
package sliceutil

import "slices"

// Unique removes duplicate elements from a slice while preserving the original order.
// It returns a new slice containing only the first occurrence of each element.
//
// The function uses a map for O(1) lookup, resulting in O(n) time complexity.
// Memory optimization: returns empty slice for empty input, returns original
// for single-element slices.
//
// Example:
//
//	nums := []int{1, 2, 2, 3, 1}
//	result := Unique(nums)  // []int{1, 2, 3}
func Unique[T comparable](slice []T) []T {
	// Edge case: empty slice
	if len(slice) == 0 {
		return []T{}
	}

	// Edge case: single element
	if len(slice) == 1 {
		return slice
	}

	seen := make(map[T]struct{}, len(slice))
	result := make([]T, 0, len(slice))

	for _, item := range slice {
		if _, exists := seen[item]; !exists {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	return result
}

// Contains checks if a slice contains the specified item.
// It returns true if the item is found, false otherwise.
//
// Time complexity: O(n) where n is the length of the slice.
//
// Example:
//
//	names := []string{"alice", "bob", "charlie"}
//	Contains(names, "bob")      // true
//	Contains(names, "dave")     // false
func Contains[T comparable](slice []T, item T) bool {
	// Edge case: empty slice
	if len(slice) == 0 {
		return false
	}

	return slices.Contains(slice, item)
}
