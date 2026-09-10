package holo

import (
	"bytes"
	"encoding/json/jsontext"
	"io"
)

// AlarmsResponse는 필수 문자열을 검증하고 공개 필드만 별도로 소유한 알람 목록입니다.
type AlarmsResponse struct {
	rows []jsontext.Value
}

func (r *AlarmsResponse) valid() bool { return r.rows != nil }

// MarshalJSONTo는 검증한 행을 현재 encoder의 UTF-8·이스케이프 정책에 따라 기록합니다.
func (r *AlarmsResponse) MarshalJSONTo(enc *jsontext.Encoder) error {
	return marshalProjectedRows(enc, "alarms", r.rows)
}

// WriteJSONTo는 검증한 비공개 행을 HTML-safe JSON으로 기록하며 I/O 실패를 전달합니다.
// 입력 검증은 UnmarshalJSONFrom이 완료하고, writer 실패 시 이미 쓴 데이터가 있을 수 있습니다.
func (r *AlarmsResponse) WriteJSONTo(w io.Writer) error {
	if r == nil {
		return errInvalidOwnedResponse
	}

	return writeProjectedRows(w, "alarms", r.rows)
}

// UnmarshalJSONFrom은 누락·null·타입 오류를 거부하고 소유하지 않은 필드를 제거합니다.
// 모든 행과 status를 확인한 뒤에만 결과를 갱신하며 reader나 작업 버퍼를 참조하지 않습니다.
func (r *AlarmsResponse) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	rows, err := decodeProjectedRows(dec, "alarms", projectAlarm)
	if err != nil {
		return err
	}

	r.rows = rows

	return nil
}

func projectAlarm(dec *jsontext.Decoder, buffer *bytes.Buffer) error {
	if err := expectJSONToken(dec, '{'); err != nil {
		return err
	}

	_ = buffer.WriteByte('{')

	var seen uint8

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		key, bit := alarmField(name.String())
		if bit == 0 {
			if err := dec.SkipValue(); err != nil {
				return err
			}

			continue
		}

		if seen != 0 {
			_ = buffer.WriteByte(',')
		}

		_, _ = buffer.WriteString(key)
		if err := copyProjectedString(dec, buffer); err != nil {
			return err
		}

		seen |= bit
	}

	if err := expectJSONToken(dec, '}'); err != nil {
		return err
	}

	if seen != 15 {
		return errInvalidOwnedResponse
	}

	_ = buffer.WriteByte('}')

	return nil
}

func alarmField(name string) (string, uint8) {
	switch name {
	case "roomId":
		return `"roomId":`, 1
	case "roomName":
		return `"roomName":`, 2
	case "channelId":
		return `"channelId":`, 4
	case "memberName":
		return `"memberName":`, 8
	default:
		return "", 0
	}
}
