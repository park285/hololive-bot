// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package util

import "fmt"

// Max: 두 정수 중 더 큰 값을 반환합니다.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min: 두 정수 중 더 작은 값을 반환합니다.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Unique: 정수 슬라이스에서 중복된 값을 제거하여 유니크한 값만 남긴다.
func Unique(nums []int) []int {
	seen := make(map[int]struct{})
	result := make([]int, 0, len(nums))

	for _, n := range nums {
		if _, exists := seen[n]; !exists {
			seen[n] = struct{}{}
			result = append(result, n)
		}
	}

	return result
}

// FormatKoreanNumber: 한국어 단위(만)로 숫자를 포맷팅합니다.
// 예: 10000 -> "1만", 12345 -> "1만 2345", 500 -> "500"
func FormatKoreanNumber(n int64) string {
	if n >= 10000 {
		man := n / 10000
		remainder := n % 10000
		if remainder == 0 {
			return fmt.Sprintf("%d만", man)
		}
		return fmt.Sprintf("%d만 %d", man, remainder)
	}
	return fmt.Sprintf("%d", n)
}
