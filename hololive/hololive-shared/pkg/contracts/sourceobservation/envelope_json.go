package sourceobservation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
)

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func decodeStrictJSON(raw []byte, destination any) error {
	if err := validateJSONStructure(raw); err != nil {
		return fmt.Errorf("validate JSON structure: %w", err)
	}

	if err := jsonv2.Unmarshal(raw, destination, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}

// validateJSONStructure는 token 한 번의 순회로 단일 최상위 값, 중첩 깊이, 문법,
// 중복 이름, UTF-8 유효성을 검증한다. 미지 member 거부는 뒤따르는 jsonv2.Unmarshal이 맡는다.
func validateJSONStructure(raw []byte) error {
	// bytes.Buffer 입력은 decoder가 복사 없이 raw를 직접 읽게 한다. decoder는 입력을 쓰지 않는다.
	decoder := jsontext.NewDecoder(bytes.NewBuffer(raw))

	for {
		if _, err := decoder.ReadToken(); err != nil {
			return fmt.Errorf("decode json: %w", err)
		}

		depth := decoder.StackDepth()
		if depth > MaxCanonicalJSONDepth {
			return fmt.Errorf("json nesting exceeds %d", MaxCanonicalJSONDepth)
		}

		if depth == 0 {
			break
		}
	}

	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode json: trailing value")
		}

		return fmt.Errorf("decode json trailing data: %w", err)
	}

	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	if err := validateCanonicalJSONStrings(value); err != nil {
		return nil, fmt.Errorf("validate canonical JSON strings: %w", err)
	}

	encoded, err := jsonv2.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	out, err := CanonicalizeJSON(encoded)
	if err != nil {
		return out, fmt.Errorf("canonicalize JSON: %w", err)
	}

	return out, nil
}
