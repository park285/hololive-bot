package sourceobservation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SchemaVersionV1                 int16 = 1
	CommunitySchemaVersionV1              = SchemaVersionV1
	MaxPayloadBytes                       = 1 << 20
	DefaultMaxSourceEventFutureSkew       = 5 * time.Minute
	MaxSourceEventFutureSkew              = 15 * time.Minute
)

type Provider string

const (
	ProviderHolodex          Provider = "holodex"
	ProviderYouTubeJS        Provider = "youtubejs"
	ProviderHololiveOfficial Provider = "hololive_official"
)

type ObservationKind string

const (
	KindCommunityPage  ObservationKind = "community_page"
	KindVideoList      ObservationKind = "video_list"
	KindShortsList     ObservationKind = "shorts_list"
	KindLiveSnapshot   ObservationKind = "live_snapshot"
	KindViewerSample   ObservationKind = "viewer_sample"
	KindChannelStats   ObservationKind = "channel_stats"
	KindChannelProfile ObservationKind = "channel_profile"
	KindChannelPhoto   ObservationKind = "channel_photo"
	KindSchedule       ObservationKind = "schedule_snapshot"
)

type Completeness string

const (
	CompletenessComplete Completeness = "COMPLETE"
	CompletenessPartial  Completeness = "PARTIAL"
	CompletenessUnknown  Completeness = "UNKNOWN"
)

type Continuity string

const (
	ContinuityContiguous    Continuity = "CONTIGUOUS"
	ContinuityGapUnresolved Continuity = "GAP_UNRESOLVED"
	ContinuityNotApplicable Continuity = "NOT_APPLICABLE"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusProcessed  Status = "PROCESSED"
	StatusDeadLetter Status = "DEAD_LETTER"
)

type LeaseProof struct {
	JobKey               string    `json:"job_key"`
	CollectionJobKind    string    `json:"collection_job_kind"`
	OwnerInstance        string    `json:"owner_instance"`
	FenceEpoch           int64     `json:"fence_epoch"`
	ProjectionGeneration int64     `json:"projection_generation"`
	ScheduledFor         time.Time `json:"scheduled_for"`
}

type Envelope struct {
	Provider           Provider        `json:"provider"`
	ObservationKind    ObservationKind `json:"observation_kind"`
	SubjectKey         string          `json:"subject_key"`
	ObservationKey     string          `json:"observation_key"`
	SchemaVersion      int16           `json:"schema_version"`
	ContractGeneration int64           `json:"contract_generation"`
	ScheduledFor       time.Time       `json:"scheduled_for"`
	ObservedAt         time.Time       `json:"observed_at"`
	SourceEventAt      *time.Time      `json:"source_event_at,omitempty"`
	ScopeSHA256        string          `json:"scope_sha256"`
	Completeness       Completeness    `json:"completeness"`
	Continuity         Continuity      `json:"continuity"`
	Payload            json.RawMessage `json:"payload"`
	PayloadSHA256      string          `json:"payload_sha256"`
	EvidenceSHA256     string          `json:"evidence_sha256"`
	CollectorInstance  string          `json:"collector_instance"`
	Lease              LeaseProof      `json:"lease"`
}

type EvidenceDigestV1 struct {
	Provider           Provider        `json:"provider"`
	ObservationKind    ObservationKind `json:"observation_kind"`
	SubjectKey         string          `json:"subject_key"`
	ObservationKey     string          `json:"observation_key"`
	SchemaVersion      int16           `json:"schema_version"`
	ContractGeneration int64           `json:"contract_generation"`
	ScheduledFor       time.Time       `json:"scheduled_for"`
	SourceEventAt      *time.Time      `json:"source_event_at,omitempty"`
	ScopeSHA256        string          `json:"scope_sha256"`
	Completeness       Completeness    `json:"completeness"`
	Continuity         Continuity      `json:"continuity"`
	PayloadSHA256      string          `json:"payload_sha256"`
}

type ObservationClock struct {
	ObservationKind ObservationKind
	ScheduledFor    time.Time
	SourceEventAt   *time.Time
	ReceivedAt      time.Time
}

var lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DecodeEnvelopeStrict(raw []byte) (Envelope, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes+8192 {
		return Envelope{}, fmt.Errorf("decode source observation envelope: size is outside the accepted range")
	}
	var envelope Envelope
	if err := decodeStrictJSON(raw, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode source observation envelope: %w", err)
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func PrepareEnvelope(envelope Envelope) (Envelope, error) {
	canonicalPayload, canonicalScope, err := canonicalPayloadAndScope(
		envelope.ObservationKind,
		envelope.SubjectKey,
		envelope.Completeness,
		envelope.Payload,
	)
	if err != nil {
		return Envelope{}, fmt.Errorf("prepare source observation envelope: %w", err)
	}
	envelope.ScheduledFor = envelope.ScheduledFor.UTC()
	envelope.ObservedAt = envelope.ObservedAt.UTC()
	if envelope.SourceEventAt != nil {
		sourceEventAt := envelope.SourceEventAt.UTC()
		envelope.SourceEventAt = &sourceEventAt
	}
	envelope.Lease.ScheduledFor = envelope.Lease.ScheduledFor.UTC()
	envelope.Payload = canonicalPayload
	envelope.ScopeSHA256 = SHA256Hex(canonicalScope)
	envelope.PayloadSHA256 = SHA256Hex(canonicalPayload)
	envelope.ObservationKey = ObservationKeyForEnvelope(envelope, canonicalScope)
	evidenceSHA256, err := envelope.expectedEvidenceSHA256()
	if err != nil {
		return Envelope{}, fmt.Errorf("prepare source observation envelope: %w", err)
	}
	envelope.EvidenceSHA256 = evidenceSHA256
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func (e Envelope) Validate() error {
	_, err := e.ValidateAndCanonicalPayload()
	return err
}

func (e Envelope) ValidateAndCanonicalPayload() ([]byte, error) {
	if !e.Provider.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: unsupported provider %q", e.Provider)
	}
	if !e.ObservationKind.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: unsupported observation kind %q", e.ObservationKind)
	}
	if err := validateBoundedText("subject key", e.SubjectKey, 256); err != nil {
		return nil, err
	}
	if err := validateBoundedText("observation key", e.ObservationKey, 512); err != nil {
		return nil, err
	}
	if e.SchemaVersion != SchemaVersionV1 {
		return nil, fmt.Errorf("validate source observation envelope: unsupported schema version %d", e.SchemaVersion)
	}
	if e.ContractGeneration <= 0 {
		return nil, fmt.Errorf("validate source observation envelope: contract generation must be positive")
	}
	if e.ScheduledFor.IsZero() || e.ObservedAt.IsZero() {
		return nil, fmt.Errorf("validate source observation envelope: scheduled for and observed at must be non-zero")
	}
	if e.SourceEventAt != nil && e.SourceEventAt.IsZero() {
		return nil, fmt.Errorf("validate source observation envelope: source event at must not point to zero time")
	}
	if e.SourceEventAt != nil && !KindAllowsSourceEventTime(e.ObservationKind) {
		return nil, fmt.Errorf("validate source observation envelope: observation kind does not allow source event time")
	}
	if !e.Completeness.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: invalid completeness %q", e.Completeness)
	}
	if !e.Continuity.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: invalid continuity %q", e.Continuity)
	}
	if err := e.Lease.validate(e.ScheduledFor); err != nil {
		return nil, err
	}
	if err := validateBoundedText("collector instance", e.CollectorInstance, 128); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{
		"scope sha256":    e.ScopeSHA256,
		"payload sha256":  e.PayloadSHA256,
		"evidence sha256": e.EvidenceSHA256,
	} {
		if !lowercaseSHA256Pattern.MatchString(value) {
			return nil, fmt.Errorf("validate source observation envelope: %s must be 64 lowercase hexadecimal characters", name)
		}
	}
	canonicalPayload, canonicalScope, err := canonicalPayloadAndScope(e.ObservationKind, e.SubjectKey, e.Completeness, e.Payload)
	if err != nil {
		return nil, fmt.Errorf("validate source observation envelope: %w", err)
	}
	if actual := SHA256Hex(canonicalScope); actual != e.ScopeSHA256 {
		return nil, fmt.Errorf("validate source observation envelope: scope sha256 mismatch")
	}
	if actual := SHA256Hex(canonicalPayload); actual != e.PayloadSHA256 {
		return nil, fmt.Errorf("validate source observation envelope: payload sha256 mismatch")
	}
	if expected := ObservationKeyForEnvelope(e, canonicalScope); expected != e.ObservationKey {
		return nil, fmt.Errorf("validate source observation envelope: observation key mismatch")
	}
	expectedEvidenceSHA256, err := e.expectedEvidenceSHA256()
	if err != nil {
		return nil, fmt.Errorf("validate source observation envelope: %w", err)
	}
	if expectedEvidenceSHA256 != e.EvidenceSHA256 {
		return nil, fmt.Errorf("validate source observation envelope: evidence sha256 mismatch")
	}
	return canonicalPayload, nil
}

func (e Envelope) expectedEvidenceSHA256() (string, error) {
	digest := EvidenceDigestV1{
		Provider:           e.Provider,
		ObservationKind:    e.ObservationKind,
		SubjectKey:         e.SubjectKey,
		ObservationKey:     e.ObservationKey,
		SchemaVersion:      e.SchemaVersion,
		ContractGeneration: e.ContractGeneration,
		ScheduledFor:       e.ScheduledFor.UTC(),
		SourceEventAt:      e.SourceEventAt,
		ScopeSHA256:        e.ScopeSHA256,
		Completeness:       e.Completeness,
		Continuity:         e.Continuity,
		PayloadSHA256:      e.PayloadSHA256,
	}
	if digest.SourceEventAt != nil {
		sourceEventAt := digest.SourceEventAt.UTC()
		digest.SourceEventAt = &sourceEventAt
	}
	canonical, err := canonicalJSON(digest)
	if err != nil {
		return "", fmt.Errorf("canonicalize evidence digest: %w", err)
	}
	return SHA256Hex(canonical), nil
}

func (proof LeaseProof) validate(scheduledFor time.Time) error {
	if err := validateBoundedText("lease job key", proof.JobKey, 512); err != nil {
		return err
	}
	if err := validateBoundedText("collection job kind", proof.CollectionJobKind, 128); err != nil {
		return err
	}
	if err := validateBoundedText("lease owner instance", proof.OwnerInstance, 128); err != nil {
		return err
	}
	if proof.FenceEpoch <= 0 || proof.ProjectionGeneration <= 0 {
		return fmt.Errorf("validate source observation envelope: lease fence and projection generation must be positive")
	}
	if proof.ScheduledFor.IsZero() || !proof.ScheduledFor.Equal(scheduledFor) {
		return fmt.Errorf("validate source observation envelope: lease scheduled slot mismatch")
	}
	return nil
}

func (p Provider) Valid() bool {
	return p == ProviderHolodex || p == ProviderYouTubeJS || p == ProviderHololiveOfficial
}

func (k ObservationKind) Valid() bool {
	switch k {
	case KindCommunityPage, KindVideoList, KindShortsList, KindLiveSnapshot,
		KindViewerSample, KindChannelStats, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	default:
		return false
	}
}

func (c Completeness) Valid() bool {
	return c == CompletenessComplete || c == CompletenessPartial || c == CompletenessUnknown
}

func (c Continuity) Valid() bool {
	return c == ContinuityContiguous || c == ContinuityGapUnresolved || c == ContinuityNotApplicable
}

func (s Status) Valid() bool {
	return s == StatusPending || s == StatusProcessing || s == StatusProcessed || s == StatusDeadLetter
}

func NegativeEligible(completeness Completeness, continuity Continuity) bool {
	return completeness == CompletenessComplete &&
		(continuity == ContinuityContiguous || continuity == ContinuityNotApplicable)
}

func KindAllowsSourceEventTime(kind ObservationKind) bool {
	switch kind {
	case KindCommunityPage, KindLiveSnapshot, KindViewerSample, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	default:
		return false
	}
}

func ValidateMaxSourceEventFutureSkew(value time.Duration) error {
	if value < 0 || value > MaxSourceEventFutureSkew {
		return fmt.Errorf("source event future skew must be between zero and %s", MaxSourceEventFutureSkew)
	}
	return nil
}

func SourceEventAtAllowed(observation ObservationClock, maxFutureSkew time.Duration) bool {
	if observation.SourceEventAt == nil || observation.ReceivedAt.IsZero() ||
		!KindAllowsSourceEventTime(observation.ObservationKind) || ValidateMaxSourceEventFutureSkew(maxFutureSkew) != nil {
		return false
	}
	return !observation.SourceEventAt.After(observation.ReceivedAt.Add(maxFutureSkew))
}

func EffectiveAt(observation ObservationClock, maxFutureSkew time.Duration) (time.Time, bool) {
	if SourceEventAtAllowed(observation, maxFutureSkew) {
		return observation.SourceEventAt.UTC(), false
	}
	return observation.ScheduledFor.UTC(), observation.SourceEventAt != nil
}

func decodeStrictJSON(raw []byte, destination any) error {
	if err := validateJSONText(raw); err != nil {
		return err
	}
	if err := rejectDuplicateJSONNames(raw); err != nil {
		return err
	}
	if err := rejectNonCanonicalJSONFields(raw, reflect.TypeOf(destination)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == nil {
		return fmt.Errorf("decode json: trailing value")
	}
	if err != io.EOF {
		return fmt.Errorf("decode json trailing data: %w", err)
	}
	return nil
}

func rejectDuplicateJSONNames(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder, 0); err != nil {
		return fmt.Errorf("validate json structure: %w", err)
	}
	return ensureJSONEOF(decoder)
}

var (
	timeType       = reflect.TypeOf(time.Time{})
	rawMessageType = reflect.TypeOf(json.RawMessage{})
)

func rejectNonCanonicalJSONFields(raw []byte, destinationType reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := inspectJSONValueWithType(decoder, indirectJSONType(destinationType)); err != nil {
		return fmt.Errorf("validate json fields: %w", err)
	}
	return ensureJSONEOF(decoder)
}

func inspectJSONValueWithType(decoder *json.Decoder, valueType reflect.Type) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if valueType == nil {
		return consumeJSONContainer(decoder, delimiter)
	}
	switch delimiter {
	case '{':
		return inspectJSONObjectFields(decoder, valueType)
	case '[':
		return inspectJSONArrayFields(decoder, valueType)
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}

func inspectJSONObjectFields(decoder *json.Decoder, valueType reflect.Type) error {
	valueType = indirectJSONType(valueType)
	if valueType == nil || valueType == rawMessageType || valueType == timeType || valueType.Kind() != reflect.Struct {
		return consumeJSONContainer(decoder, '{')
	}
	fields := jsonFieldTypes(valueType)
	seen := make(map[string]struct{}, len(fields))
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := nameToken.(string)
		if !ok {
			return fmt.Errorf("object field name is not a string")
		}
		fieldType, ok := fields[name]
		if !ok {
			return fmt.Errorf("field %q is not a canonical field name", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate object field %q", name)
		}
		seen[name] = struct{}{}
		if err := inspectJSONValueWithType(decoder, fieldType); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func inspectJSONArrayFields(decoder *json.Decoder, valueType reflect.Type) error {
	valueType = indirectJSONType(valueType)
	itemType := reflect.TypeOf((*any)(nil)).Elem()
	if valueType != nil && (valueType.Kind() == reflect.Array || valueType.Kind() == reflect.Slice) {
		itemType = valueType.Elem()
	}
	for decoder.More() {
		if err := inspectJSONValueWithType(decoder, itemType); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func consumeJSONContainer(decoder *json.Decoder, opening json.Delim) error {
	closing := json.Delim('}')
	if opening == '[' {
		closing = ']'
	}
	for decoder.More() {
		if err := inspectJSONValueWithType(decoder, nil); err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil {
		return err
	} else if token != closing {
		return fmt.Errorf("unexpected delimiter %q", token)
	}
	return nil
}

func indirectJSONType(valueType reflect.Type) reflect.Type {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

func jsonFieldTypes(valueType reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, valueType.NumField())
	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func validateJSONText(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("decode json: invalid UTF-8")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		end, err := validateJSONString(raw, i+1)
		if err != nil {
			return err
		}
		i = end
	}
	return nil
}

func validateJSONString(raw []byte, start int) (int, error) {
	for i := start; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			return i, nil
		case '\\':
			end, err := validateJSONEscape(raw, i)
			if err != nil {
				return 0, err
			}
			i = end
		default:
			if raw[i] < 0x20 {
				return 0, fmt.Errorf("decode json: invalid control character in string")
			}
		}
	}
	return 0, fmt.Errorf("decode json: unterminated string")
}

func validateJSONEscape(raw []byte, start int) (int, error) {
	if start+1 >= len(raw) {
		return 0, fmt.Errorf("decode json: unterminated escape sequence")
	}
	if raw[start+1] != 'u' {
		switch raw[start+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			return start + 1, nil
		default:
			return 0, fmt.Errorf("decode json: invalid escape sequence")
		}
	}
	if start+6 > len(raw) {
		return 0, fmt.Errorf("decode json: incomplete Unicode escape")
	}
	codeUnit, ok := parseJSONHex4(raw[start+2 : start+6])
	if !ok {
		return 0, fmt.Errorf("decode json: invalid Unicode escape")
	}
	if codeUnit >= 0xD800 && codeUnit <= 0xDBFF {
		return validateJSONLowSurrogate(raw, start)
	}
	if codeUnit >= 0xDC00 && codeUnit <= 0xDFFF {
		return 0, fmt.Errorf("decode json: low surrogate is not preceded by a high surrogate")
	}
	return start + 5, nil
}

func validateJSONLowSurrogate(raw []byte, highStart int) (int, error) {
	lowStart := highStart + 6
	if lowStart+6 > len(raw) || raw[lowStart] != '\\' || raw[lowStart+1] != 'u' {
		return 0, fmt.Errorf("decode json: high surrogate is not followed by a low surrogate")
	}
	low, ok := parseJSONHex4(raw[lowStart+2 : lowStart+6])
	if !ok || low < 0xDC00 || low > 0xDFFF {
		return 0, fmt.Errorf("decode json: high surrogate is not followed by a low surrogate")
	}
	return lowStart + 5, nil
}

func parseJSONHex4(raw []byte) (uint16, bool) {
	if len(raw) != 4 {
		return 0, false
	}
	var value uint16
	for _, digit := range raw {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value += uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value += uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		if depth >= MaxCanonicalJSONDepth {
			return fmt.Errorf("json nesting exceeds %d", MaxCanonicalJSONDepth)
		}
		return inspectJSONNames(decoder, depth+1)
	case '[':
		if depth >= MaxCanonicalJSONDepth {
			return fmt.Errorf("json nesting exceeds %d", MaxCanonicalJSONDepth)
		}
		return inspectJSONArray(decoder, depth+1)
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}

func inspectJSONNames(decoder *json.Decoder, depth int) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := nameToken.(string)
		if !ok {
			return fmt.Errorf("object field name is not a string")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate object field %q", name)
		}
		seen[name] = struct{}{}
		if err := inspectJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func inspectJSONArray(decoder *json.Decoder, depth int) error {
	for decoder.More() {
		if err := inspectJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func canonicalJSON(value any) ([]byte, error) {
	if err := validateCanonicalJSONStrings(value); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return CanonicalizeJSON(encoded)
}

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func validateBoundedText(name, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate source observation envelope: %s is empty", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("validate source observation envelope: %s exceeds %d bytes", name, maxLength)
	}
	if trimmed != value {
		return fmt.Errorf("validate source observation envelope: %s must not contain surrounding whitespace", name)
	}
	return nil
}
