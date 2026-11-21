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

export function recordScheduleLookup() {
  const store = storage.getStore();
  if (store != null) store.scheduleLookups = (store.scheduleLookups ?? 0) + 1;
}

export function currentScheduleLookups() {
  return storage.getStore()?.scheduleLookups ?? 0;
}

/**
 * @template T
 * @param {() => T} fn
 * @returns {T}
 */
export function runWithoutRequestContext(fn) {
  return storage.exit(fn);
}
