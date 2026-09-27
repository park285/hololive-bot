// @ts-check
import { AsyncLocalStorage } from "node:async_hooks";

/** @typedef {{ requestId: string, signal: AbortSignal }} RequestContext */

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

/**
 * 공유 발급 작업은 특정 collection RPC의 취소·식별자를 상속하지 않습니다.
 * @template T
 * @param {() => T} fn
 * @returns {T}
 */
export function runWithoutRequestContext(fn) {
  return storage.exit(fn);
}
