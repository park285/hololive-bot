import type { Continuity, TerminationReason } from "./contracts.d.ts";

export const continuityContiguous: "CONTIGUOUS";
export const continuityGap: "GAP_UNRESOLVED";
export const continuityNotApplicable: "NOT_APPLICABLE";
export const maxCursorJSONBytes: 8192;

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
  cursorStart?: string;
  cursorEnd?: string;
  reason: TerminationReason;
  continuity?: Continuity;
}): {
  page_count: number;
  cursor_start?: string;
  cursor_end?: string;
  exhausted: boolean;
  continuity: Continuity;
  termination_reason: TerminationReason;
};

export function paginate<T, R>(options: {
  firstPage: unknown;
  getContinuation?: (feed: any) => Promise<unknown>;
  mapPage: (feed: any) => { recognized_shape: true; items: T[] };
  maxPages?: number;
  maxResults?: number;
  maxSuccessResponseBytes?: number;
  reservedEnvelopeBytes: number;
  buildResult?: (items: T[], pagination: ReturnType<typeof paginationResult>) => R;
}): Promise<R>;
