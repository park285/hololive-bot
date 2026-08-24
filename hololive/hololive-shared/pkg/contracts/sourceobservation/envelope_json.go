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

func validateJSONStructure(raw []byte) error {
	if err := validateSingleJSONValue(raw); err != nil {
		return fmt.Errorf("validate single JSON value: %w", err)
	}

	if err := validateJSONDepth(raw); err != nil {
		return fmt.Errorf("validate JSON depth: %w", err)
	}

	return nil
}

func validateSingleJSONValue(raw []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.ReadValue(); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	if _, err := decoder.ReadValue(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode json: trailing value")
		}

		return fmt.Errorf("decode json trailing data: %w", err)
	}

	return nil
}

func validateJSONDepth(raw []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))

	for {
		if _, err := decoder.ReadToken(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("decode json: %w", err)
		}

		if decoder.StackDepth() > MaxCanonicalJSONDepth {
			return fmt.Errorf("json nesting exceeds %d", MaxCanonicalJSONDepth)
		}
	}
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
