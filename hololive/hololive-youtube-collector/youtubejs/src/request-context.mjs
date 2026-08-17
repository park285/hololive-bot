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
