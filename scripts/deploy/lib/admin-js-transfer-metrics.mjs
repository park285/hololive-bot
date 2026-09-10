import assert from "node:assert/strict";

/** scriptTransfers는 cache·취소를 보존하며 Chrome이 실제 받은 script body bytes를 기록합니다. */
export async function scriptTransfers(context, page) {
  const session = await context.newCDPSession(page), records = new Map();
  await session.send("Network.enable");
  session.on("Network.requestWillBeSent", event => {
    if (event.type === "Script") records.set(event.requestId, { path: new URL(event.request.url).pathname, bytes: 0, decoded_bytes: 0, cached: false, completed: false });
  });
  session.on("Network.responseReceived", event => {
    const record = records.get(event.requestId); if (!record) return;
    record.status = event.response.status;
    record.cached ||= Boolean(event.response.fromDiskCache || event.response.fromPrefetchCache);
    const headers = Object.fromEntries(Object.entries(event.response.headers).map(([name, value]) => [name.toLowerCase(), value]));
    record.encoding = headers["content-encoding"] ?? "identity";
    if (headers["content-length"] !== undefined) record.content_length = Number(headers["content-length"]);
  });
  session.on("Network.requestServedFromCache", event => { const record = records.get(event.requestId); if (record) record.cached = true; });
  session.on("Network.dataReceived", event => {
    const record = records.get(event.requestId); if (!record) return;
    record.bytes += event.encodedDataLength; record.decoded_bytes += event.dataLength;
  });
  session.on("Network.loadingFinished", event => { const record = records.get(event.requestId); if (record) { record.completed = true; record.total_encoded_with_headers = event.encodedDataLength; } });
  session.on("Network.loadingFailed", event => { const record = records.get(event.requestId); if (record) record.failure = { canceled: Boolean(event.canceled), reason: event.errorText }; });
  return () => {
    const transfers = [...records.values()];
    for (const transfer of transfers) {
      assert(transfer.bytes >= 0 && Number.isFinite(transfer.bytes));
      if (transfer.failure) assert(transfer.failure.canceled, JSON.stringify(transfer));
      else assert(transfer.completed && [200, 304].includes(transfer.status), JSON.stringify(transfer));
      if (transfer.completed && transfer.status === 200 && !transfer.cached && transfer.content_length !== undefined) assert.equal(transfer.bytes, transfer.content_length, JSON.stringify(transfer));
    }
    return { bytes: transfers.reduce((total, transfer) => total + transfer.bytes, 0), transfers };
  };
}
