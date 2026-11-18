package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"golang.org/x/text/secure/precis"
)

func pgxSCRAMPassword(password string) string {
	prepared, err := precis.OpaqueString.String(password)
	if err != nil {
		return password
	}

	return prepared
}

// pgxdb.SCRAMVerifier가 PostgreSQL·pgx 정규화 일치를 검증한 비밀번호만 받는다.
func matchesValidatedSCRAMPassword(password, verifier string) (bool, error) {
	encoded, scram := strings.CutPrefix(verifier, "SCRAM-SHA-256$")
	if !scram {
		return false, nil
	}

	parameters, keys, ok := strings.Cut(encoded, "$")
	if !ok {
		return false, errors.New("stored SCRAM verifier is malformed")
	}

	iterationText, saltText, ok := strings.Cut(parameters, ":")
	if !ok {
		return false, errors.New("stored SCRAM verifier is malformed")
	}

	iterations, err := strconv.Atoi(iterationText)
	// pgx의 서버 제공 iteration 상한과 같아야 bootstrap에서 계산량이 무제한이 되지 않는다.
	if err != nil || iterations <= 0 || iterations > 10_000_000 {
		return false, errors.New("stored SCRAM iteration count is invalid")
	}

	salt, err := base64.StdEncoding.DecodeString(saltText)
	if err != nil || len(salt) == 0 {
		return false, errors.New("stored SCRAM salt is malformed")
	}

	storedText, serverText, ok := strings.Cut(keys, ":")
	if !ok {
		return false, errors.New("stored SCRAM verifier is malformed")
	}

	storedKey, err := base64.StdEncoding.DecodeString(storedText)
	if err != nil || len(storedKey) != sha256.Size {
		return false, errors.New("stored SCRAM client key is malformed")
	}

	serverKey, err := base64.StdEncoding.DecodeString(serverText)
	if err != nil || len(serverKey) != sha256.Size {
		return false, errors.New("stored SCRAM server key is malformed")
	}

	salted, err := pbkdf2.Key(sha256.New, pgxSCRAMPassword(password), salt, iterations, sha256.Size)
	if err != nil {
		return false, errors.New("derive stored SCRAM key failed")
	}

	mac := hmac.New(sha256.New, salted)

	_, _ = mac.Write([]byte("Client Key"))

	var clientKey [sha256.Size]byte

	computedStoredKey := sha256.Sum256(mac.Sum(clientKey[:0]))
	mac.Reset()

	_, _ = mac.Write([]byte("Server Key"))

	var computedServerKey [sha256.Size]byte

	mac.Sum(computedServerKey[:0])

	storedMatches := hmac.Equal(storedKey, computedStoredKey[:])
	serverMatches := hmac.Equal(serverKey, computedServerKey[:])

	return storedMatches && serverMatches, nil
}
