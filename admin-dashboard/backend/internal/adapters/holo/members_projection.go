package holo

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
	"strconv"
)

const (
	memberIDSeen uint8 = 1 << iota
	memberChannelSeen
	memberNameSeen
	memberGraduatedSeen
	memberAliasesSeen

	requiredMemberFields = memberIDSeen | memberChannelSeen | memberNameSeen | memberGraduatedSeen
	emptyMemberAliases   = `{"ko":[],"ja":[]}`
)

// MembersResponse는 정수 ID와 필수 필드를 검증하고 공개 필드만 소유한 멤버 목록입니다.
type MembersResponse struct {
	rows []jsontext.Value
}

func (r *MembersResponse) valid() bool { return r.rows != nil }

// MarshalJSONTo는 검증한 멤버 행을 현재 encoder의 UTF-8·이스케이프 정책에 따라 기록합니다.
func (r *MembersResponse) MarshalJSONTo(enc *jsontext.Encoder) error {
	return marshalProjectedRows(enc, "members", r.rows)
}

// WriteJSONTo는 검증한 비공개 멤버 행을 HTML-safe JSON으로 기록하며 I/O 실패를 전달합니다.
// 정수·필수 값·UTF-8 검사는 완료된 상태이며 writer 실패 시 이미 쓴 데이터가 있을 수 있습니다.
func (r *MembersResponse) WriteJSONTo(w io.Writer) error {
	if r == nil {
		return errInvalidOwnedResponse
	}

	return writeProjectedRows(w, "members", r.rows)
}

// UnmarshalJSONFrom은 필수 값과 별명 배열을 검사하고 정수 ID를 손실 없는 문자열로 바꿉니다.
// 선택 이름의 null·빈 문자열은 DTO의 omitempty대로 생략하고, 부재·null인 별명 객체는 빈 배열로 투영합니다.
func (r *MembersResponse) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	rows, err := decodeProjectedRows(dec, "members", projectMemberJSON)
	if err != nil {
		return err
	}

	r.rows = rows

	return nil
}

func projectMemberJSON(dec *jsontext.Decoder, buffer *bytes.Buffer) error {
	if err := expectJSONToken(dec, '{'); err != nil {
		return err
	}

	_ = buffer.WriteByte('{')

	var seen uint8

	written := false

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		bit, err := projectMemberField(dec, buffer, name.String(), &written)
		if err != nil {
			return err
		}

		seen |= bit
	}

	if err := expectJSONToken(dec, '}'); err != nil {
		return err
	}

	if seen&requiredMemberFields != requiredMemberFields {
		return errInvalidOwnedResponse
	}

	if seen&memberAliasesSeen == 0 {
		appendProjectedName(buffer, `"aliases":`, &written)

		_, _ = buffer.WriteString(emptyMemberAliases)
	}

	_ = buffer.WriteByte('}')

	return nil
}

func projectMemberField(dec *jsontext.Decoder, buffer *bytes.Buffer, name string, written *bool) (uint8, error) {
	switch name {
	case "id":
		return memberIDSeen, projectMemberID(dec, buffer, written)
	case "channelId":
		return memberChannelSeen, projectMemberString(dec, buffer, `"channelId":`, written, false)
	case "name":
		return memberNameSeen, projectMemberString(dec, buffer, `"name":`, written, false)
	case "isGraduated":
		return memberGraduatedSeen, projectGraduation(dec, buffer, written)
	case "nameJa":
		return 0, projectMemberString(dec, buffer, `"nameJa":`, written, true)
	case "nameKo":
		return 0, projectMemberString(dec, buffer, `"nameKo":`, written, true)
	case "aliases":
		return memberAliasesSeen, projectMemberAliases(dec, buffer, written)
	default:
		return 0, dec.SkipValue()
	}
}

func projectMemberID(dec *jsontext.Decoder, buffer *bytes.Buffer, written *bool) error {
	var id int64

	if err := jsonv2.UnmarshalDecode(dec, &id); err != nil {
		return err
	}

	if id <= 0 {
		return errInvalidOwnedResponse
	}

	appendProjectedName(buffer, `"id":`, written)

	_ = buffer.WriteByte('"')

	var digits [20]byte

	_, _ = buffer.Write(strconv.AppendInt(digits[:0], id, 10))
	_ = buffer.WriteByte('"')

	return nil
}

func projectGraduation(dec *jsontext.Decoder, buffer *bytes.Buffer, written *bool) error {
	value, err := dec.ReadValue()
	if err != nil {
		return err
	}

	if kind := value.Kind(); kind != 't' && kind != 'f' {
		return errInvalidOwnedResponse
	}

	appendProjectedName(buffer, `"isGraduated":`, written)

	_, _ = buffer.Write(value)

	return nil
}

func projectMemberString(dec *jsontext.Decoder, buffer *bytes.Buffer, key string, written *bool, nullable bool) error {
	if nullable && dec.PeekKind() == 'n' {
		return expectJSONToken(dec, 'n')
	}

	value, err := readProjectedString(dec)
	if err != nil {
		return err
	}

	// contract.Member의 json/v2 omitempty는 non-nil 포인터의 빈 문자열도 생략합니다.
	if nullable && len(value) == 2 {
		return nil
	}

	appendProjectedName(buffer, key, written)

	_, _ = buffer.Write(value)

	return nil
}

func projectMemberAliases(dec *jsontext.Decoder, buffer *bytes.Buffer, written *bool) error {
	appendProjectedName(buffer, `"aliases":`, written)

	if dec.PeekKind() == 'n' {
		if err := expectJSONToken(dec, 'n'); err != nil {
			return err
		}

		_, _ = buffer.WriteString(emptyMemberAliases)

		return nil
	}

	return projectAliasObject(dec, buffer)
}

func projectAliasObject(dec *jsontext.Decoder, buffer *bytes.Buffer) error {
	if err := expectJSONToken(dec, '{'); err != nil {
		return err
	}

	_ = buffer.WriteByte('{')

	var seen uint8

	written := false

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		key, bit := aliasField(name.String())
		if bit == 0 {
			if err := dec.SkipValue(); err != nil {
				return err
			}

			continue
		}

		appendProjectedName(buffer, key, &written)

		if err := projectAliasArray(dec, buffer); err != nil {
			return err
		}

		seen |= bit
	}

	if err := expectJSONToken(dec, '}'); err != nil {
		return err
	}

	if seen != 3 {
		return errInvalidOwnedResponse
	}

	_ = buffer.WriteByte('}')

	return nil
}

func aliasField(name string) (string, uint8) {
	switch name {
	case "ko":
		return `"ko":`, 1
	case "ja":
		return `"ja":`, 2
	default:
		return "", 0
	}
}

func projectAliasArray(dec *jsontext.Decoder, buffer *bytes.Buffer) error {
	if err := expectJSONToken(dec, '['); err != nil {
		return err
	}

	_ = buffer.WriteByte('[')

	first := true

	for dec.PeekKind() != ']' {
		if !first {
			_ = buffer.WriteByte(',')
		}

		first = false

		if err := copyProjectedString(dec, buffer); err != nil {
			return err
		}
	}

	if err := expectJSONToken(dec, ']'); err != nil {
		return err
	}

	_ = buffer.WriteByte(']')

	return nil
}
