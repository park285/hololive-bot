package sourceobservation

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxFailureDetailBytes = 2048

type CollectionErrorCode string

const (
	ErrorCollectionFailed       CollectionErrorCode = "collection_failed"
	ErrorCollectionTimeout      CollectionErrorCode = "collection_timeout"
	ErrorCollectionCanceled     CollectionErrorCode = "collection_canceled"
	ErrorParserDrift            CollectionErrorCode = "parser_drift"
	ErrorCooldown               CollectionErrorCode = "cooldown"
	ErrorConfiguration          CollectionErrorCode = "configuration_error"
	ErrorResponseTooLarge       CollectionErrorCode = "response_too_large"
	ErrorHelperBusy             CollectionErrorCode = "helper_busy"
	ErrorHelperProtocolMismatch CollectionErrorCode = "helper_protocol_mismatch"
	ErrorInternalInvariant      CollectionErrorCode = "collection_internal_invariant"
	ErrorTargetRosterTooLarge   CollectionErrorCode = "target_roster_too_large"
	ErrorPublishRejected        CollectionErrorCode = "publish_rejected"
	ErrorObservationCollision   CollectionErrorCode = "observation_collision"
	ErrorShutdownRelease        CollectionErrorCode = "shutdown_release"
	ErrorSupersededRelease      CollectionErrorCode = "superseded_release"
	ErrorRenewFailedRelease     CollectionErrorCode = "renew_failed_release"
)

type FailureClass string

const (
	ClassTransient     FailureClass = "TRANSIENT"
	ClassTimeout       FailureClass = "TIMEOUT"
	ClassCanceled      FailureClass = "CANCELED"
	ClassCooldown      FailureClass = "COOLDOWN"
	ClassDataContract  FailureClass = "DATA_CONTRACT"
	ClassResourceLimit FailureClass = "RESOURCE_LIMIT"
	ClassConfiguration FailureClass = "CONFIGURATION"
	ClassProtocol      FailureClass = "PROTOCOL"
	ClassSuperseded    FailureClass = "SUPERSEDED"
	ClassInternal      FailureClass = "INTERNAL"
)

type CollectionTerminalTransition string

const (
	TerminalDefer         CollectionTerminalTransition = "DEFER"
	TerminalRelease       CollectionTerminalTransition = "RELEASE"
	TerminalCompleteError CollectionTerminalTransition = "COMPLETE_WITH_ERROR"
)

type FailureTuple struct {
	Code  CollectionErrorCode
	Class FailureClass
}

type FailureDiagnostic struct {
	code   CollectionErrorCode
	class  FailureClass
	detail string
}

func NewFailureDiagnostic(
	code CollectionErrorCode,
	class FailureClass,
	detail string,
) (FailureDiagnostic, error) {
	diagnostic := FailureDiagnostic{
		code:   code,
		class:  class,
		detail: strings.TrimSpace(detail),
	}
	if err := diagnostic.Validate(); err != nil {
		return FailureDiagnostic{}, err
	}
	return diagnostic, nil
}

func (d FailureDiagnostic) Code() CollectionErrorCode { return d.code }
func (d FailureDiagnostic) Class() FailureClass       { return d.class }
func (d FailureDiagnostic) Detail() string            { return d.detail }

func (d FailureDiagnostic) Validate() error {
	if !ValidDurableFailureTuple(d.code, d.class) {
		return fmt.Errorf("validate failure diagnostic: invalid code/class")
	}
	return validateFailureDetail(d.detail)
}

func (d FailureDiagnostic) ValidateFor(transition CollectionTerminalTransition) error {
	if err := validateFailureTransition(transition); err != nil {
		return err
	}
	if err := d.Validate(); err != nil {
		return err
	}
	return validateFailureCodeForTransition(d.code, transition)
}

func validateFailureTransition(transition CollectionTerminalTransition) error {
	switch transition {
	case TerminalRelease:
		return fmt.Errorf("validate failure diagnostic: release does not persist a diagnostic")
	case TerminalDefer, TerminalCompleteError:
		return nil
	default:
		return fmt.Errorf("validate failure diagnostic: unknown transition %q", transition)
	}
}

func validateFailureCodeForTransition(code CollectionErrorCode, transition CollectionTerminalTransition) error {
	if transition == TerminalDefer {
		if !code.Deferable() {
			return fmt.Errorf("validate failure diagnostic: code %q is not deferable", code)
		}
		return nil
	}
	if !code.CompletesWithError() {
		return fmt.Errorf("validate failure diagnostic: code %q does not complete with error", code)
	}
	return nil
}

func (c CollectionErrorCode) Valid() bool {
	_, ok := collectionErrorCodeSet[c]
	return ok
}

func (c CollectionErrorCode) Deferable() bool {
	_, ok := deferableCodeSet[c]
	return ok
}

func (c CollectionErrorCode) Releasable() bool {
	_, ok := releasableCodeSet[c]
	return ok
}

func (c CollectionErrorCode) CompletesWithError() bool {
	_, ok := completeErrorCodeSet[c]
	return ok
}

func (c FailureClass) Valid() bool {
	_, ok := failureClassSet[c]
	return ok
}

func ValidDurableFailureTuple(code CollectionErrorCode, class FailureClass) bool {
	_, ok := durableTupleSet[FailureTuple{Code: code, Class: class}]
	return ok
}

func AllCollectionErrorCodes() []CollectionErrorCode {
	return cloneCodes(allCollectionErrorCodes)
}

func AllFailureClasses() []FailureClass {
	return cloneClasses(allFailureClasses)
}

func DeferFailureTuples() []FailureTuple {
	return cloneTuples(deferFailureTuples)
}

func CompleteErrorFailureTuples() []FailureTuple {
	return cloneTuples(completeErrorFailureTuples)
}

func AllDurableFailureTuples() []FailureTuple {
	return cloneTuples(allDurableFailureTuples)
}

func DeferableCollectionErrorCodes() []CollectionErrorCode {
	return cloneCodes(deferableCollectionErrorCodes)
}

func ReleasableCollectionErrorCodes() []CollectionErrorCode {
	return cloneCodes(releasableCollectionErrorCodes)
}

func CompletesWithErrorCodes() []CollectionErrorCode {
	return cloneCodes(completesWithErrorCodes)
}

func DefaultFailureClass(code CollectionErrorCode) (FailureClass, bool) {
	class, ok := defaultFailureClassByCode[code]
	return class, ok
}

func validateFailureDetail(detail string) error {
	if !utf8.ValidString(detail) {
		return fmt.Errorf("validate failure diagnostic: detail is not valid UTF-8")
	}
	if strings.IndexByte(detail, 0) >= 0 {
		return fmt.Errorf("validate failure diagnostic: detail contains NUL")
	}
	if n := len(detail); n < 1 || n > maxFailureDetailBytes {
		return fmt.Errorf("validate failure diagnostic: detail must be 1..%d bytes", maxFailureDetailBytes)
	}
	return nil
}

func sortedCopyCodes(codes []CollectionErrorCode) []CollectionErrorCode {
	out := cloneCodes(codes)
	slices.Sort(out)
	return out
}

func sortedCopyClasses(classes []FailureClass) []FailureClass {
	out := cloneClasses(classes)
	slices.Sort(out)
	return out
}

func sortedCopyTuples(tuples []FailureTuple) []FailureTuple {
	out := cloneTuples(tuples)
	slices.SortFunc(out, compareFailureTuple)
	return out
}

func cloneCodes(codes []CollectionErrorCode) []CollectionErrorCode {
	out := slices.Clone(codes)
	if out == nil {
		return []CollectionErrorCode{}
	}
	return out
}

func cloneClasses(classes []FailureClass) []FailureClass {
	out := slices.Clone(classes)
	if out == nil {
		return []FailureClass{}
	}
	return out
}

func cloneTuples(tuples []FailureTuple) []FailureTuple {
	out := slices.Clone(tuples)
	if out == nil {
		return []FailureTuple{}
	}
	return out
}

func compareFailureTuple(a, b FailureTuple) int {
	if a.Code != b.Code {
		if a.Code < b.Code {
			return -1
		}
		return 1
	}
	if a.Class == b.Class {
		return 0
	}
	if a.Class < b.Class {
		return -1
	}
	return 1
}

func setFromCodes(codes []CollectionErrorCode) map[CollectionErrorCode]struct{} {
	out := make(map[CollectionErrorCode]struct{}, len(codes))
	for _, code := range codes {
		out[code] = struct{}{}
	}
	return out
}

func setFromClasses(classes []FailureClass) map[FailureClass]struct{} {
	out := make(map[FailureClass]struct{}, len(classes))
	for _, class := range classes {
		out[class] = struct{}{}
	}
	return out
}

func setFromTuples(tuples []FailureTuple) map[FailureTuple]struct{} {
	out := make(map[FailureTuple]struct{}, len(tuples))
	for _, tuple := range tuples {
		out[tuple] = struct{}{}
	}
	return out
}

func uniqueCodes(tuples []FailureTuple) []CollectionErrorCode {
	seen := make(map[CollectionErrorCode]struct{}, len(tuples))
	codes := make([]CollectionErrorCode, 0, len(tuples))
	for _, tuple := range tuples {
		if _, ok := seen[tuple.Code]; ok {
			continue
		}
		seen[tuple.Code] = struct{}{}
		codes = append(codes, tuple.Code)
	}
	slices.Sort(codes)
	return codes
}

var (
	deferFailureTuples = sortedCopyTuples([]FailureTuple{
		{ErrorCollectionFailed, ClassTransient},
		{ErrorCollectionFailed, ClassProtocol},
		{ErrorCollectionTimeout, ClassTimeout},
		{ErrorCollectionCanceled, ClassCanceled},
		{ErrorParserDrift, ClassDataContract},
		{ErrorCooldown, ClassCooldown},
		{ErrorConfiguration, ClassConfiguration},
		{ErrorResponseTooLarge, ClassResourceLimit},
		{ErrorHelperBusy, ClassTransient},
		{ErrorHelperProtocolMismatch, ClassProtocol},
		{ErrorInternalInvariant, ClassInternal},
		{ErrorTargetRosterTooLarge, ClassResourceLimit},
		{ErrorPublishRejected, ClassTransient},
		{ErrorPublishRejected, ClassProtocol},
		{ErrorPublishRejected, ClassInternal},
	})
	completeErrorFailureTuples = sortedCopyTuples([]FailureTuple{
		{ErrorObservationCollision, ClassDataContract},
	})
	allDurableFailureTuples        = sortedCopyTuples(append(slices.Clone(deferFailureTuples), completeErrorFailureTuples...))
	deferableCollectionErrorCodes  = uniqueCodes(deferFailureTuples)
	completesWithErrorCodes        = uniqueCodes(completeErrorFailureTuples)
	releasableCollectionErrorCodes = sortedCopyCodes([]CollectionErrorCode{
		ErrorShutdownRelease,
		ErrorSupersededRelease,
		ErrorRenewFailedRelease,
	})
	allCollectionErrorCodes = sortedCopyCodes(append(append(
		slices.Clone(deferableCollectionErrorCodes),
		completesWithErrorCodes...),
		releasableCollectionErrorCodes...))
	allFailureClasses = sortedCopyClasses([]FailureClass{
		ClassTransient,
		ClassTimeout,
		ClassCanceled,
		ClassCooldown,
		ClassDataContract,
		ClassResourceLimit,
		ClassConfiguration,
		ClassProtocol,
		ClassSuperseded,
		ClassInternal,
	})
	collectionErrorCodeSet    = setFromCodes(allCollectionErrorCodes)
	failureClassSet           = setFromClasses(allFailureClasses)
	deferableCodeSet          = setFromCodes(deferableCollectionErrorCodes)
	releasableCodeSet         = setFromCodes(releasableCollectionErrorCodes)
	completeErrorCodeSet      = setFromCodes(completesWithErrorCodes)
	durableTupleSet           = setFromTuples(allDurableFailureTuples)
	defaultFailureClassByCode = buildDefaultFailureClass(allDurableFailureTuples)
)

func buildDefaultFailureClass(tuples []FailureTuple) map[CollectionErrorCode]FailureClass {
	byCode := make(map[CollectionErrorCode][]FailureClass, len(tuples))
	for _, tuple := range tuples {
		byCode[tuple.Code] = append(byCode[tuple.Code], tuple.Class)
	}
	out := make(map[CollectionErrorCode]FailureClass, len(byCode))
	for code, classes := range byCode {
		if class, ok := defaultFailureClass(code, classes); ok {
			out[code] = class
		}
	}
	return out
}

func defaultFailureClass(code CollectionErrorCode, classes []FailureClass) (FailureClass, bool) {
	if len(classes) == 1 {
		return classes[0], true
	}
	if code == ErrorCollectionFailed || code == ErrorPublishRejected {
		return ClassTransient, true
	}
	return "", false
}
