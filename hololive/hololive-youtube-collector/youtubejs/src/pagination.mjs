export const continuityContiguous = "CONTIGUOUS";
export const continuityGap = "GAP_UNRESOLVED";

export function continuationToken(feed) {
  if (feed == null) {
    return "";
  }
  const token = feed.continuation ?? feed.continuation_token ?? feed.continuationToken ?? "";
  if (typeof token === "string") {
    return token.trim();
  }
  if (typeof token?.token === "string") {
    return token.token.trim();
  }
  return "";
}

export function hasContinuation(feed) {
  if (feed == null) {
    return false;
  }
  if (feed.has_continuation === true || feed.hasContinuation === true) {
    return true;
  }
  return continuationToken(feed) !== "";
}

export function encodedSize(value) {
  return Buffer.byteLength(JSON.stringify(value), "utf8");
}

export function paginationResult({
  pageCount,
  cursorStart,
  cursorEnd,
  exhausted,
  looped = false,
  truncated = false,
}) {
  const gap = looped || truncated || !exhausted;
  return {
    page_count: pageCount,
    cursor_start: cursorStart || undefined,
    cursor_end: cursorEnd || undefined,
    exhausted: exhausted === true && !looped && !truncated,
    continuity: gap ? continuityGap : continuityContiguous,
  };
}

export async function paginate({
  firstPage,
  getContinuation,
  mapPage,
  maxPages,
  maxResults,
  maxAggregateBytes,
}) {
  const pages = Math.max(1, Math.trunc(Number(maxPages) || 1));
  const resultLimit = Number.isFinite(maxResults) && maxResults > 0 ? Math.trunc(maxResults) : 0;
  const byteLimit =
    Number.isFinite(maxAggregateBytes) && maxAggregateBytes > 0 ? Math.trunc(maxAggregateBytes) : 0;
  const items = [];
  const seen = new Set();
  let pageCount = 0;
  let cursorStart = "";
  let cursorEnd = "";
  let exhausted = false;
  let looped = false;
  let truncated = false;
  let feed = firstPage;
  while (feed != null && pageCount < pages) {
    const mapped = mapPage(feed);
    if (!Array.isArray(mapped)) {
      const err = new Error("helper page mapper returned a non-array");
      err.code = "parser_drift";
      throw err;
    }
    pageCount += 1;
    const cursor = continuationToken(feed);
    if (pageCount === 1) {
      cursorStart = cursor;
    }
    cursorEnd = cursor;
    for (const item of mapped) {
      if (resultLimit > 0 && items.length >= resultLimit) {
        truncated = true;
        break;
      }
      items.push(item);
      if (byteLimit > 0 && encodedSize(items) > byteLimit) {
        items.pop();
        truncated = true;
        break;
      }
    }
    if (truncated) {
      break;
    }
    if (!hasContinuation(feed)) {
      exhausted = true;
      break;
    }
    if (pageCount >= pages) {
      truncated = true;
      break;
    }
    if (cursor !== "" && seen.has(cursor)) {
      looped = true;
      truncated = true;
      break;
    }
    if (cursor !== "") {
      seen.add(cursor);
    }
    try {
      feed = await getContinuation(feed);
    } catch (err) {
      if (err?.code === "parser_drift") {
        throw err;
      }
      if (pageCount >= 1) {
        truncated = true;
        break;
      }
      throw err;
    }
  }
  if (pageCount < 1) {
    const err = new Error("helper produced no validated page");
    err.code = "parser_drift";
    throw err;
  }
  return {
    items,
    ...paginationResult({
      pageCount,
      cursorStart,
      cursorEnd,
      exhausted,
      looped,
      truncated,
    }),
  };
}
