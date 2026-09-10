package holo

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"sync"
)

const maxRetainedProjectionBuffer = 1 << 20

var projectionBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func writeProjectedRows(w io.Writer, collection string, rows []jsontext.Value) error {
	if rows == nil {
		return errInvalidOwnedResponse
	}

	if _, err := io.WriteString(w, `{"status":"ok","`+collection+`":[`); err != nil {
		return fmt.Errorf("write projected header: %w", err)
	}

	for index, row := range rows {
		if index > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return fmt.Errorf("write projected row separator: %w", err)
			}
		}

		if err := writeHTMLSafeRow(w, row); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "]}"); err != nil {
		return fmt.Errorf("write projected end: %w", err)
	}

	return nil
}

func writeHTMLSafeRow(w io.Writer, row jsontext.Value) error {
	// 행은 strict decoder와 고정 필드 생성기를 통과했고 비공개로 소유합니다.
	// 유효한 JSON에서 이 ASCII 문자는 문자열 안에만 있으므로 escape만 추가해 의미를 보존합니다.
	for {
		index := bytes.IndexAny(row, "<>&")
		if index < 0 {
			if _, err := w.Write(row); err != nil {
				return fmt.Errorf("write projected row: %w", err)
			}

			return nil
		}

		if _, err := w.Write(row[:index]); err != nil {
			return fmt.Errorf("write projected row prefix: %w", err)
		}

		var escaped string

		switch row[index] {
		case '<':
			escaped = `\u003c`
		case '>':
			escaped = `\u003e`
		case '&':
			escaped = `\u0026`
		}

		if _, err := io.WriteString(w, escaped); err != nil {
			return fmt.Errorf("write HTML escape: %w", err)
		}

		row = row[index+1:]
	}
}

func marshalProjectedRows(enc *jsontext.Encoder, collection string, rows []jsontext.Value) error {
	if rows == nil {
		return errInvalidOwnedResponse
	}

	for _, token := range []jsontext.Token{jsontext.BeginObject, jsontext.String("status"), jsontext.String("ok"), jsontext.String(collection), jsontext.BeginArray} {
		if err := enc.WriteToken(token); err != nil {
			return fmt.Errorf("write projected list header: %w", err)
		}
	}

	// 행별로 기록해야 큰 전체 JSON을 encoder의 임시 버퍼에 다시 확장하지 않습니다.
	for _, row := range rows {
		if err := enc.WriteValue(row); err != nil {
			return fmt.Errorf("write projected row: %w", err)
		}
	}

	for _, token := range []jsontext.Token{jsontext.EndArray, jsontext.EndObject} {
		if err := enc.WriteToken(token); err != nil {
			return fmt.Errorf("write projected list end: %w", err)
		}
	}

	return nil
}

func decodeProjectedRows(dec *jsontext.Decoder, collection string, project func(*jsontext.Decoder, *bytes.Buffer) error) ([]jsontext.Value, error) {
	// json/v2는 사용자 decoder의 I/O 오류를 직접 타입으로 식별합니다. 이 경계의 decoder 오류를
	// 감싸면 SemanticError로 바뀌어 외부 입력 오류를 숨기는 상위 계층에서 원인이 유실됩니다.
	allowDuplicate, _ := jsonv2.GetOption(dec.Options(), jsontext.AllowDuplicateNames)
	allowInvalidUTF8, _ := jsonv2.GetOption(dec.Options(), jsontext.AllowInvalidUTF8)

	if allowDuplicate || allowInvalidUTF8 {
		return nil, errInvalidOwnedResponse
	}

	buffer, ok := projectionBuffers.Get().(*bytes.Buffer)
	if !ok || buffer == nil {
		return nil, errors.New("invalid projection buffer pool entry")
	}

	buffer.Reset()

	defer func() {
		if buffer.Cap() <= maxRetainedProjectionBuffer {
			buffer.Reset()
			projectionBuffers.Put(buffer)
		}
	}()

	if err := expectJSONToken(dec, '{'); err != nil {
		return nil, err
	}

	var (
		seen uint8
		rows []jsontext.Value
	)

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}

		switch name.String() {
		case "status":
			if statusErr := readProjectedStatus(dec); statusErr != nil {
				return nil, statusErr
			}

			seen |= 1
		case collection:
			rows, err = projectRowsArray(dec, buffer, project)
			if err != nil {
				return nil, err
			}

			seen |= 2
		default:
			if err := dec.SkipValue(); err != nil {
				return nil, err
			}
		}
	}

	if err := expectJSONToken(dec, '}'); err != nil {
		return nil, err
	}

	if seen != 3 {
		return nil, errInvalidOwnedResponse
	}

	return rows, nil
}

func readProjectedStatus(dec *jsontext.Decoder) error {
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}

	if token.Kind() != '"' || token.String() != "ok" {
		return errInvalidOwnedResponse
	}

	return nil
}

func projectRowsArray(dec *jsontext.Decoder, buffer *bytes.Buffer, project func(*jsontext.Decoder, *bytes.Buffer) error) ([]jsontext.Value, error) {
	if err := expectJSONToken(dec, '['); err != nil {
		return nil, err
	}

	// nil은 미검증 상태이고, 유효한 빈 목록은 반드시 non-nil입니다.
	rows := make([]jsontext.Value, 0)

	var storage []byte

	for dec.PeekKind() != ']' {
		buffer.Reset()

		if err := project(dec, buffer); err != nil {
			return nil, err
		}

		// 응답 소유의 작은 블록에 행을 나눠 복사해 행별 할당을 줄입니다.
		// 이미 보존한 구간은 다시 쓰지 않고 cap도 고정해 reader·다른 행과 분리합니다.
		if len(storage) < buffer.Len() {
			storage = make([]byte, max(16<<10, buffer.Len()))
		}

		length := copy(storage, buffer.Bytes())

		rows = append(rows, storage[:length:length])
		storage = storage[length:]
	}

	if err := expectJSONToken(dec, ']'); err != nil {
		return nil, err
	}

	return rows, nil
}

func expectJSONToken(dec *jsontext.Decoder, kind jsontext.Kind) error {
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}

	if token.Kind() != kind {
		return errInvalidOwnedResponse
	}

	return nil
}

func appendProjectedName(buffer *bytes.Buffer, key string, written *bool) {
	if *written {
		_ = buffer.WriteByte(',')
	}

	_, _ = buffer.WriteString(key)
	*written = true
}

func copyProjectedString(dec *jsontext.Decoder, buffer *bytes.Buffer) error {
	value, err := readProjectedString(dec)
	if err != nil {
		return err
	}

	_, _ = buffer.Write(value)

	return nil
}

func readProjectedString(dec *jsontext.Decoder) (jsontext.Value, error) {
	value, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}

	// PeekKind의 0에는 I/O 오류도 포함되므로 값을 읽어 원인을 확인한 뒤 타입을 검사합니다.
	if value.Kind() != '"' {
		return nil, errInvalidOwnedResponse
	}

	return value, nil
}
