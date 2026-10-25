// @ts-check
import { AsyncLocalStorage } from "node:async_hooks";

/** @typedef {{ requestId: string, signal: AbortSignal, scheduleLookups?: number }} RequestContext */

const storage = new AsyncLocalStorage();

/**
 * @template T
 * @param {RequestContext} context
 * @param {() => T} fn
 * @returns {T}
 */
export function runWithRequestContext(context, fn) {
  return storage.run(context, fn);
}

export function currentRequestSignal() {
  return storage.getStore()?.signal;
}

/** 현재 collection RPC가 보낸 예정 영상 player 조회를 셉니다. 실패 응답도 collector 호출 예산에 반영되게 합니다. */
export function recordScheduleLookup() {
  const store = storage.getStore();
  if (store != null) store.scheduleLookups = (store.scheduleLookups ?? 0) + 1;
}

export function currentScheduleLookups() {
  return storage.getStore()?.scheduleLookups ?? 0;
}

/**
 * 공유 발급 작업은 특정 collection RPC의 취소·식별자를 상속하지 않습니다.
 * @template T
 * @param {() => T} fn
 * @returns {T}
 */
export function runWithoutRequestContext(fn) {
  return storage.exit(fn);
}
