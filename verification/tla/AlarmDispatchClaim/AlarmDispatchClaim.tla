---- MODULE AlarmDispatchClaim ----
EXTENDS Naturals, FiniteSets

CONSTANTS Workers, LastTick, LeaseTTL, StaleAfter
VARIABLES clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
          pc, claimClock, unitLock, calls, invalidBegin, invalidSettlement

vars == <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
          pc, claimClock, unitLock, calls, invalidBegin, invalidSettlement>>

Init == /\ clock = 0 /\ status = "retry" /\ lockedBy = "" /\ expiresAt = 0
        /\ sendingStartedAt = 0 /\ sentAtSet = FALSE
        /\ pc = [w \in Workers |-> "ready"] /\ claimClock = [w \in Workers |-> 0]
        /\ unitLock = "" /\ calls = {} /\ invalidBegin = FALSE /\ invalidSettlement = FALSE

ClockTick == /\ clock < LastTick /\ clock' = clock + 1
             /\ UNCHANGED <<status, lockedBy, expiresAt, sendingStartedAt, sentAtSet, pc,
                            claimClock, unitLock, calls, invalidBegin, invalidSettlement>>

ClaimSnapshot(w) == /\ pc[w] = "ready" /\ status \in {"pending", "retry"}
                    /\ pc' = [pc EXCEPT ![w] = "snapshot"]
                    /\ UNCHANGED <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
                                   claimClock, unitLock, calls, invalidBegin, invalidSettlement>>

SampleClaimClock(w) == /\ pc[w] = "snapshot"
                       /\ claimClock' = [claimClock EXCEPT ![w] = clock]
                       /\ pc' = [pc EXCEPT ![w] = "candidate"]
                       /\ UNCHANGED <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
                                      unitLock, calls, invalidBegin, invalidSettlement>>

LockSendUnit(w) == /\ pc[w] = "candidate" /\ unitLock = "" /\ unitLock' = w
                   /\ pc' = [pc EXCEPT ![w] = "picked"]
                   /\ UNCHANGED <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
                                  claimClock, calls, invalidBegin, invalidSettlement>>

SkipLocked(w) == /\ pc[w] = "candidate" /\ unitLock # ""
                 /\ pc' = [pc EXCEPT ![w] = "done"]
                 /\ UNCHANGED <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
                                claimClock, unitLock, calls, invalidBegin, invalidSettlement>>

ClaimDue(w) == /\ pc[w] = "picked" /\ unitLock = w
               /\ status' = "leased" /\ lockedBy' = w /\ expiresAt' = claimClock[w] + LeaseTTL
               /\ unitLock' = "" /\ pc' = [pc EXCEPT ![w] = "claimed"]
               /\ UNCHANGED <<clock, sendingStartedAt, sentAtSet, claimClock, calls, invalidBegin, invalidSettlement>>

BeginSendRequest(w) == /\ pc[w] = "claimed" /\ unitLock = ""
                       /\ status = "leased" /\ lockedBy = w /\ expiresAt > clock
                       /\ status' = "sending" /\ sendingStartedAt' = clock /\ expiresAt' = clock + LeaseTTL
                       /\ pc' = [pc EXCEPT ![w] = "authorized"]
                       /\ invalidBegin' = (lockedBy # w \/ expiresAt <= clock \/ status # "leased")
                       /\ UNCHANGED <<clock, lockedBy, sentAtSet, claimClock, unitLock, calls, invalidSettlement>>

SendPinnedRequest(w) == /\ pc[w] = "authorized" /\ calls' = calls \cup {w}
                        /\ pc' = [pc EXCEPT ![w] = "sentRequest"]
                        /\ UNCHANGED <<clock, status, lockedBy, expiresAt, sendingStartedAt, sentAtSet,
                                       claimClock, unitLock, invalidBegin, invalidSettlement>>

MarkSent(w) == /\ pc[w] = "sentRequest" /\ unitLock = ""
               /\ status = "sending" /\ lockedBy = w
               /\ status' = "sent" /\ sentAtSet' = TRUE /\ lockedBy' = "" /\ expiresAt' = 0
               /\ pc' = [pc EXCEPT ![w] = "done"]
               /\ invalidSettlement' = (lockedBy # w \/ status # "sending")
               /\ UNCHANGED <<clock, sendingStartedAt, claimClock, unitLock, calls, invalidBegin>>

RecoverExpiredLeased == /\ unitLock = "" /\ status = "leased" /\ expiresAt < clock
                        /\ status' = "retry" /\ lockedBy' = "" /\ expiresAt' = 0
                        /\ UNCHANGED <<clock, sendingStartedAt, sentAtSet, pc, claimClock, unitLock,
                                       calls, invalidBegin, invalidSettlement>>

QuarantineStaleSending == /\ status = "sending" /\ clock > sendingStartedAt + StaleAfter
                          /\ status' = "quarantined" /\ lockedBy' = "" /\ expiresAt' = 0
                          /\ UNCHANGED <<clock, sendingStartedAt, sentAtSet, pc, claimClock, unitLock,
                                         calls, invalidBegin, invalidSettlement>>

Next == \/ ClockTick \/ RecoverExpiredLeased \/ QuarantineStaleSending
        \/ \E w \in Workers : ClaimSnapshot(w) \/ SampleClaimClock(w) \/ LockSendUnit(w) \/ SkipLocked(w)
                              \/ ClaimDue(w) \/ BeginSendRequest(w) \/ SendPinnedRequest(w) \/ MarkSent(w)
        \/ UNCHANGED vars

Spec == Init /\ [][Next]_vars
SendNeedsLiveLease == ~invalidBegin
SettlementNeedsSendingOwner == ~invalidSettlement
SettledStaysSent == sentAtSet => status = "sent"
AtMostOneTransportCall == Cardinality(calls) <= 1
====
