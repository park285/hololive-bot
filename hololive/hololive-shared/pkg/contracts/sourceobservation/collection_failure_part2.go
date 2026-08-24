package sourceobservation

import (
	"slices"
)

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
