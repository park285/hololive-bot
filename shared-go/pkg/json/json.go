package json

import (
	"encoding/json"
	"io"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/decoder"
)

// 표준 라이브러리 타입 호환성
type (
	// Sonic returns specific error types. Aliasing them allows some compatibility.
	SyntaxError        = decoder.SyntaxError
	UnmarshalTypeError = decoder.MismatchTypeError

	// Standard types used by Sonic
	RawMessage = json.RawMessage
	Number     = json.Number

	// Stream Encoders/Decoders Interfaces
	Encoder = sonic.Encoder
	Decoder = sonic.Decoder
)

// ConfigDefault는 기본 설정을 사용하는 API 인스턴스입니다.
var api = sonic.ConfigDefault

func Marshal(v any) ([]byte, error) {
	return api.Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return api.Unmarshal(data, v)
}

func NewEncoder(w io.Writer) Encoder {
	return api.NewEncoder(w)
}

func NewDecoder(r io.Reader) Decoder {
	return api.NewDecoder(r)
}

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return api.MarshalIndent(v, prefix, indent)
}

func Valid(data []byte) bool {
	return api.Valid(data)
}
