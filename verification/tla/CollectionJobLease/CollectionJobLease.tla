---- MODULE CollectionJobLease ----
EXTENDS Integers, Sequences

CONSTANTS Owners, LastTick, MaxGrants, LeaseTTL, PollInterval
VARIABLES clock, slotState, ownerInstance, fenceEpoch, scheduledFor, nextDueAt,
          leaseExpiresAt, retryNotBefore, proofs, completedThrough, earlyGrant, staleWrite

vars == <<clock, slotState, ownerInstance, fenceEpoch, scheduledFor, nextDueAt,
          leaseExpiresAt, retryNotBefore, proofs, completedThrough, earlyGrant, staleWrite>>

Init ==
    /\ clock = 0
    /\ slotState = "IDLE"
    /\ ownerInstance = ""
    /\ fenceEpoch = 1
    /\ scheduledFor = -PollInterval
    /\ nextDueAt = 0
    /\ leaseExpiresAt = -1
    /\ retryNotBefore = -1
    /\ proofs = <<>>
    /\ completedThrough = scheduledFor
    /\ earlyGrant = FALSE
    /\ staleWrite = FALSE

ClockTick == /\ clock < LastTick
             /\ clock' = clock + 1
             /\ UNCHANGED <<slotState, ownerInstance, fenceEpoch, scheduledFor, nextDueAt,
                            leaseExpiresAt, retryNotBefore, proofs, completedThrough, earlyGrant, staleWrite>>

IdleDue == IF nextDueAt < scheduledFor + PollInterval THEN nextDueAt ELSE scheduledFor + PollInterval

Acquire(w) ==
    /\ Len(proofs) < MaxGrants
    /\ \/ /\ slotState = "IDLE" /\ IdleDue <= clock
       \/ /\ slotState = "DEFERRED" /\ retryNotBefore <= clock
       \/ /\ slotState = "ACTIVE" /\ leaseExpiresAt <= clock
    /\ ownerInstance = "" \/ leaseExpiresAt <= clock
    /\ slotState' = "ACTIVE"
    /\ ownerInstance' = w
    /\ fenceEpoch' = fenceEpoch + 1
    /\ scheduledFor' = IF slotState = "IDLE" THEN clock - ((clock - IdleDue) % PollInterval) ELSE scheduledFor
    /\ leaseExpiresAt' = clock + LeaseTTL
    /\ retryNotBefore' = -1
    /\ proofs' = Append(proofs, [owner |-> w, epoch |-> fenceEpoch', slot |-> scheduledFor'])
    /\ earlyGrant' = CASE slotState = "ACTIVE" -> clock < leaseExpiresAt
                       [] slotState = "DEFERRED" -> clock < retryNotBefore
                       [] OTHER -> FALSE
    /\ UNCHANGED <<clock, nextDueAt, completedThrough, staleWrite>>

ActiveProof(i) == /\ slotState = "ACTIVE" /\ ownerInstance = proofs[i].owner
                  /\ fenceEpoch = proofs[i].epoch /\ scheduledFor = proofs[i].slot
                  /\ leaseExpiresAt > clock

RecordWrite(i) == staleWrite' = (i # Len(proofs) \/ clock >= leaseExpiresAt \/ slotState # "ACTIVE")

Renew(i) == /\ ActiveProof(i) /\ RecordWrite(i)
            /\ leaseExpiresAt' = clock + LeaseTTL
            /\ UNCHANGED <<clock, slotState, ownerInstance, fenceEpoch, scheduledFor, nextDueAt,
                           retryNotBefore, proofs, completedThrough, earlyGrant>>

ClearOwner == /\ ownerInstance' = "" /\ leaseExpiresAt' = -1

CompleteCurrent(i) == /\ ActiveProof(i) /\ RecordWrite(i) /\ ClearOwner
                      /\ slotState' = "IDLE" /\ retryNotBefore' = -1
                      /\ nextDueAt' = scheduledFor + PollInterval
                      /\ completedThrough' = scheduledFor
                      /\ UNCHANGED <<clock, fenceEpoch, scheduledFor, proofs, earlyGrant>>

DelayRetry(delay) == /\ ClearOwner /\ slotState' = "DEFERRED"
                     /\ retryNotBefore' = clock + delay
                     /\ UNCHANGED <<clock, fenceEpoch, scheduledFor, nextDueAt, proofs, completedThrough, earlyGrant>>

Defer(i) == /\ ActiveProof(i) /\ RecordWrite(i) /\ DelayRetry(2)

Release(i) == /\ ActiveProof(i) /\ RecordWrite(i) /\ DelayRetry(1)

ReleaseSuperseded(i) == /\ ActiveProof(i) /\ RecordWrite(i) /\ ClearOwner
                        /\ slotState' = "IDLE" /\ retryNotBefore' = -1
                        /\ nextDueAt' = IF nextDueAt <= clock THEN nextDueAt ELSE clock
                        /\ UNCHANGED <<clock, fenceEpoch, scheduledFor, proofs, completedThrough, earlyGrant>>

Next == \/ ClockTick
        \/ \E w \in Owners : Acquire(w)
        \/ \E i \in 1..Len(proofs) : Renew(i) \/ CompleteCurrent(i) \/ Defer(i) \/ Release(i) \/ ReleaseSuperseded(i)
        \/ UNCHANGED vars

Spec == Init /\ [][Next]_vars
NoEarlyGrant == ~earlyGrant
OnlyCurrentUnexpiredProofWrites == ~staleWrite
CompletedSlotsStayClosed == slotState = "ACTIVE" => scheduledFor > completedThrough
====
