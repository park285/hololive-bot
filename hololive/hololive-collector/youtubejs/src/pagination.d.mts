import type { Continuity, Pagination, TerminationReason } from "./contracts.d.ts";

export const continuityContiguous: "CONTIGUOUS";
export const continuityGap: "GAP_UNRESOLVED";
export const continuityNotApplicable: "NOT_APPLICABLE";
export const maxPagesLimit: 100;
export const maxResultsLimit: 10_000;

export class EncodedArrayBudget<T = unknown> {
  constructor(limitBytes: number, reservedEnvelopeBytes: number);
  tryAppend(value: T): "APPENDED" | "WOULD_EXCEED";
  values(): T[];
  encodedItemsBytes(): number;
  count(): number;
}

export function continuationToken(feed: unknown): string;
export function hasContinuation(feed: unknown): boolean;
export function encodedSize(value: unknown): number;
export function paginationEnvelopeReserve(skeleton: Record<string, unknown>): number;
export function assertResponseBudget(limitBytes: number, reservedEnvelopeBytes: number): void;
export function paginationResult(options: {
  pageCount: number;
  reason: TerminationReason;
  continuity?: Continuity;
}): Pagination;
export function validatedPagination(value: {
  page_count: unknown;
  exhausted: unknown;
  continuity: unknown;
  termination_reason: unknown;
}): Pagination;

export function paginate<T, R>(options: {
  firstPage: unknown;
  getContinuation?: (feed: any) => Promise<unknown>;
  mapPage: (feed: any) =>
    | { recognized_shape: true; items: Iterable<T> | AsyncIterable<T> }
    | Promise<{ recognized_shape: true; items: Iterable<T> | AsyncIterable<T> }>;
  maxPages: number;
  maxResults: number;
  maxSuccessResponseBytes?: number;
  reservedEnvelopeBytes: number;
  buildResult?: (items: T[], pagination: Pagination) => R;
}): Promise<R>;
