package sourceobservation

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func canonicalObjectMembers(values map[string]any) []canonicalJSONMember {
	members := make([]canonicalJSONMember, 0, len(values))
	for name, memberValue := range values {
		members = append(members, canonicalJSONMember{
			name: name, order: utf16.Encode([]rune(name)), value: memberValue,
		})
	}

	sort.Slice(members, func(i, j int) bool {
		return lessUTF16(members[i].order, members[j].order)
	})

	return members
}

func appendCanonicalJSONString(destination []byte, value string) []byte {
	destination = append(destination, '"')

	for _, character := range value {
		destination = appendEscapedJSONRune(destination, character)
	}

	return append(destination, '"')
}

func appendEscapedJSONRune(destination []byte, character rune) []byte {
	switch character {
	case '"':
		return append(destination, '\\', '"')
	case '\\':
		return append(destination, '\\', '\\')
	default:
		return appendEscapedJSONControl(destination, character)
	}
}

func appendEscapedJSONControl(destination []byte, character rune) []byte {
	switch character {
	case '\b':
		return append(destination, '\\', 'b')
	case '\t':
		return append(destination, '\\', 't')
	case '\n':
		return append(destination, '\\', 'n')
	default:
		return appendEscapedJSONFormOrCarriageReturn(destination, character)
	}
}

func appendEscapedJSONFormOrCarriageReturn(destination []byte, character rune) []byte {
	switch character {
	case '\f':
		return append(destination, '\\', 'f')
	case '\r':
		return append(destination, '\\', 'r')
	default:
		return appendJSONControlOrRune(destination, character)
	}
}

func appendJSONControlOrRune(destination []byte, character rune) []byte {
	const hexadecimal = "0123456789abcdef"

	if character < 0x20 {
		return append(
			destination,
			'\\', 'u', '0', '0',
			hexadecimal[character>>4],
			hexadecimal[character&0x0f],
		)
	}

	return utf8.AppendRune(destination, character)
}

func lessUTF16(left, right []uint16) bool {
	limit := min(len(left), len(right))
	for index := range limit {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}

	return len(left) < len(right)
}

func canonicalizeIntegerJSONNumber(raw string) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	unsigned, exponentText, fractionalPart := splitJSONNumber(raw)
	digits := strings.TrimLeft(unsigned+fractionalPart, "0")

	if digits == "" {
		return "0", nil
	}

	exponent, err := parseJSONNumberExponent(exponentText)
	if err != nil {
		return "", fmt.Errorf("parse JSON number exponent: %w", err)
	}

	scaled, err := scaleJSONIntegerDigits(digits, exponent-int64(len(fractionalPart)))
	if err != nil {
		return "", fmt.Errorf("scale JSON integer digits: %w", err)
	}

	if exceedsSafeJSONInteger(scaled) {
		return "", errors.New("canonical json integer exceeds the safe range")
	}

	if negative {
		return "-" + scaled, nil
	}

	return scaled, nil
}

func splitJSONNumber(raw string) (unsigned, exponentText, fractionalPart string) {
	unsigned = strings.TrimPrefix(raw, "-")
	if exponentIndex := strings.IndexAny(unsigned, "eE"); exponentIndex >= 0 {
		exponentText = unsigned[exponentIndex+1:]
		unsigned = unsigned[:exponentIndex]
	}

	if decimalIndex := strings.IndexByte(unsigned, '.'); decimalIndex >= 0 {
		fractionalPart = unsigned[decimalIndex+1:]
		unsigned = unsigned[:decimalIndex]
	}

	return unsigned, exponentText, fractionalPart
}

func parseJSONNumberExponent(exponentText string) (int64, error) {
	if exponentText == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseInt(exponentText, 10, 32)
	if err != nil {
		return 0, errors.New("canonical json number exponent is outside the accepted range")
	}

	return parsed, nil
}

func scaleJSONIntegerDigits(digits string, scale int64) (string, error) {
	if scale < 0 {
		out, err := shrinkJSONIntegerDigits(digits, -scale)
		if err != nil {
			return out, fmt.Errorf("shrink JSON integer digits: %w", err)
		}

		return out, nil
	}

	if int64(len(digits))+scale > int64(len(maxCanonicalSafeIntegerString)) {
		return "", errors.New("canonical json integer exceeds the safe range")
	}

	return digits + strings.Repeat("0", int(scale)), nil
}

func shrinkJSONIntegerDigits(digits string, fractionalDigits int64) (string, error) {
	if fractionalDigits > int64(len(digits)) {
		return "", errors.New("canonical json numbers must have an integer value")
	}

	integerEnd := len(digits) - int(fractionalDigits)
	if strings.Trim(digits[integerEnd:], "0") != "" {
		return "", errors.New("canonical json numbers must have an integer value")
	}

	digits = strings.TrimLeft(digits[:integerEnd], "0")
	if digits == "" {
		return "0", nil
	}

	return digits, nil
}

func exceedsSafeJSONInteger(digits string) bool {
	return len(digits) > len(maxCanonicalSafeIntegerString) ||
		len(digits) == len(maxCanonicalSafeIntegerString) && digits > maxCanonicalSafeIntegerString
}
