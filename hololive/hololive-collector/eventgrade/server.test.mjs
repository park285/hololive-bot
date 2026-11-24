import { test } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { request } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import http2 from 'node:http2';
import { checkGrpcStatus, collect, createGradeServer, decryptProto, encryptProto, gradeObservation, parseEventCatalog, parseGradeRows, parseRequest, validBootstrap, validCollect, validateGradeRows } from './server.mjs';

const config = () => ({ version: 1, secret: { app_version: 'test', api_secret: 'fixture-only', credentials: { jp: 'one', us: 'two', as: 'three' } } });

test('bootstrap enforces independent exact region credentials', () => {
  assert.equal(validBootstrap(config()), true);
  for (const change of [c => c.version++, c => delete c.secret.credentials.us, c => c.secret.api_secret = '', c => c.secret.credentials.jp = c.secret.credentials.as, c => c.secret.credentials.global = 'x', c => c.endpoint = 'x', c => c.secret.app_version = 1]) {
    const c = config(); change(c); assert.equal(validBootstrap(c), false);
  }
});

test('only fixed collection operations and canonical regions are accepted', () => {
  const req = { version: 1, operation: 'catalog', region: 'jp' };
  assert.equal(validCollect(req), true);
  assert.equal(validCollect({ ...req, region: 'global' }), false);
  assert.equal(validCollect({ ...req, host: 'other' }), false);
  const border = { ...req, operation: 'border', event_id: 'e', chapter_id: 'c', final_after: '2026-11-20T00:00:00Z', binding_observed_at: '2026-11-19T00:00:00Z' };
  assert.equal(validCollect(border), true);
  const ack = { ...req, operation: 'ack', conflicts: ['c'] };
  assert.equal(validCollect(ack), true);
  assert.equal(validCollect({ ...ack, conflicts: ['c', 'c'] }), false);
  assert.equal(validCollect({ ...border, final_after: 'invalid' }), false);
  assert.equal(validCollect({ ...req, operation: 'border', event_id: 'e' }), false);
  assert.equal(validCollect({ ...req, snapshot: {} }), false);
});

test('body limits, duplicate keys and malformed UTF-8 are rejected', () => {
  for (const raw of [Buffer.alloc(16385), Buffer.from('{"x":1,"x":2}'), Buffer.from('{"x":1,"\\u0078":2}'), Buffer.from('{"a":{"y":1,"y":2}}'),
    Buffer.from('[{"z":1},{"y":[1,{"q":1,"q":1}]}]'), Buffer.from([0xff])]) assert.throws(() => parseRequest(raw));
});

test('any valid JSON encoding of a request is accepted', () => {
  const expected = { version: 1, operation: 'ack', region: 'jp', conflicts: ['a', 'b'] };
  for (const raw of ['{"version":1,"operation":"ack","region":"jp","conflicts":["a","b"]}',
    '{ "region" : "jp", "conflicts": [ "a", "b" ], "operation": "ack", "version": 1.0 }',
    '{"version":1,"operation":"\\u0061ck","region":"jp","conflicts":["a","b"]}',
    '{"version":1,"operation":"ack","region":"jp","conflicts":["a","b"],"x\\"y":{"version":2}}']) {
    const parsed = parseRequest(Buffer.from(raw));
    if (Object.hasOwn(parsed, 'x"y')) delete parsed['x"y'];
    assert.deepEqual(parsed, expected);
  }
});

function varint(value) {
  const output = [];
  let current = BigInt(value);
  while (current >= 128n) {
    output.push(Number(current & 127n) | 128);
    current >>= 7n;
  }
  output.push(Number(current));
  return Buffer.from(output);
}

function integer(tag, value) {
  return Buffer.concat([varint(tag << 3), varint(value)]);
}

function message(tag, value) {
  return Buffer.concat([varint(tag << 3 | 2), varint(value.length), value]);
}

function text(tag, value) {
  return message(tag, Buffer.from(value));
}

test('proto-enc matches independent empty, AES-CBC and gzip wire vectors', () => {
  const ticks = 638000000000000000n;
  const key = 'fixture-secret';
  const empty = Buffer.from('0000000000', 'hex');
  const plain = Buffer.from('000000001c0a0000080000b3a69ea1da08d72c0c244a3d9f1f226ffac545ad26c8', 'hex');
  const gzip = Buffer.from('000000003c0a0001080000b3a69ea1da089284f17a848fae2661df72eb27f5a3a4b099251ebfd1ee9b507ceeece8939d2e3810a15fe0d7d45bc6c44f1913afaebe', 'hex');
  assert.deepEqual(encryptProto(Buffer.alloc(0), key, ticks), empty);
  assert.deepEqual(decryptProto(empty, key), Buffer.alloc(0));
  assert.deepEqual(encryptProto(Buffer.from('payload'), key, ticks), plain);
  assert.deepEqual(decryptProto(plain, key), Buffer.from('payload'));
  assert.deepEqual(decryptProto(gzip, key), Buffer.from('hello '.repeat(400)));
  for (const size of [2048, 2049]) {
    const payload = Buffer.alloc(size, 7);
    const frame = encryptProto(payload, key, ticks);
    assert.equal(frame[7], Number(size > 2048));
    assert.deepEqual(decryptProto(frame, key), payload);
  }
});

test('outer compression, unknown inner modes and excessive inner output fail closed', () => {
  const frame = encryptProto(Buffer.from('payload'), 'test');
  for (const [offset, value] of [[0, 1], [0, 255], [7, 2], [8, 9]]) {
    const changed = Buffer.from(frame);
    changed[offset] = value;
    assert.throws(() => decryptProto(changed, 'test'));
  }
  const oversized = encryptProto(Buffer.alloc(2 * 1024 * 1024 + 1), 'test');
  assert.throws(() => decryptProto(oversized, 'test'));
});

test('event catalog binds relay chapter schedule and accepts regional dynamic grade ranks', () => {
  const chapter = Buffer.concat([text(1, 'chapter_1'), integer(4, 1760003600), integer(15, 1760007200)]);
  const marathon = Buffer.concat([integer(3, 1760007200), message(4, chapter)]);
  const event = Buffer.concat([text(1, 'event_1'), integer(2, 2), integer(4, 1760000000), integer(5, 1760003600), message(14, marathon)]);
  assert.deepEqual(parseEventCatalog(message(1, event)), [{
    event_id: 'event_1',
    chapter_id: 'chapter_1',
    event_type: 'relay',
    starts_at: '2025-10-09T08:53:20.000Z',
    ends_at: '2025-10-09T09:53:20.000Z',
    event_ends_at: '2025-10-09T09:53:20.000Z',
    reveals_at: '2025-10-09T10:53:20.000Z',
  }]);
  const rank = (position, score) => message(4, Buffer.concat([integer(1, position), integer(2, score)]));
  const rows = parseGradeRows(message(1, Buffer.concat([rank(10000, 20), rank(1000, 100), rank(5000, 70)])));
  assert.deepEqual(validateGradeRows(rows), [{ rank: 1000, score: '100' }, { rank: 5000, score: '70' }, { rank: 10000, score: '20' }]);
  assert.throws(() => validateGradeRows([...rows, rows[1]]));
  assert.throws(() => validateGradeRows([{ rank: 500, score: '10' }, { rank: 1000, score: '20' }]));
  const noStartChapter = message(4, Buffer.concat([text(1, 'without_start'), integer(4, 200)]));
  const noStart = Buffer.concat([text(1, 'event_without_start'), integer(2, 1), integer(5, 200), message(14, Buffer.concat([integer(3, 300), noStartChapter]))]);
  assert.deepEqual(parseEventCatalog(message(1, noStart)), []);
});

const chapterOf = (id, end) => message(4, Buffer.concat([text(1, id), integer(4, end)]));
const eventOf = (id, ...chapters) => message(1, Buffer.concat([
  text(1, id), integer(2, 1), integer(4, 100), integer(5, 200),
  message(14, Buffer.concat([integer(3, 300), ...chapters])),
]));
const catalogProto = eventOf('e', chapterOf('c', 200));
const gradeProto = message(1, message(4, integer(1, 1000)));
const loginPath = '/rpc.api.Auth/Login';
const masterPath = '/rpc.api.Master/Get';
const catalogPath = '/rpc.api.Event/ListEventInfo';
const gradePath = '/rpc.api.Marathon/ListMarathonScoreRankingGrade';
const catalogRequest = { version: 1, region: 'jp', operation: 'catalog' };
const borderRequest = { ...catalogRequest, operation: 'border', chapter_id: 'c', event_id: 'e', final_after: '2026-11-20T00:00:00Z', binding_observed_at: '2026-11-19T00:00:00Z' };
const success = { ':status': 200, 'content-type': 'application/grpc+proto-enc', 'grpc-status': '0' };

test('consumed protobuf fields reject the wrong wire type, including fixed-width bytes', () => {
  const fixed32 = tag => Buffer.concat([varint(tag << 3 | 5), Buffer.alloc(4)]);
  for (const proto of [
    fixed32(1), message(1, integer(1, 1)), message(1, text(2, 'wrong')),
    message(1, text(4, 'wrong')), message(1, text(5, 'wrong')), message(1, integer(14, 0)),
    message(1, message(14, text(3, 'wrong'))), message(1, message(14, fixed32(4))),
    message(1, message(14, text(5, 'wrong'))), eventOf('e', message(4, integer(1, 0))),
    eventOf('e', message(4, text(4, 'wrong'))), eventOf('e', message(4, text(15, 'wrong'))),
  ]) assert.throws(() => parseEventCatalog(proto), /protobuf_type/);
  for (const proto of [
    fixed32(1), message(1, fixed32(4)), message(1, message(4, text(1, 'wrong'))),
    message(1, message(4, Buffer.concat([integer(1, 100), text(2, 'wrong')]))),
  ]) assert.throws(() => parseGradeRows(proto), /protobuf_type/);
});

test('protobuf record and field budgets are enforced before parsing child records', () => {
  const repeat = (field, count) => Buffer.concat(Array(count).fill(field));
  assert.throws(() => parseEventCatalog(repeat(message(1, Buffer.alloc(0)), 129)), /protobuf_limit/);
  assert.throws(() => parseEventCatalog(eventOf('e', repeat(chapterOf('c', 200), 129))), /protobuf_limit/);
  assert.throws(() => parseGradeRows(message(1, repeat(message(4, Buffer.alloc(0)), 33))), /protobuf_limit/);
  assert.throws(() => parseGradeRows(repeat(integer(99, 0), 1025)), /protobuf_limit/);
  assert.throws(() => parseGradeRows(Buffer.concat([gradeProto, gradeProto])), /protobuf_duplicate/);
});

async function localRegion(t, respond) {
  const upstream = http2.createServer();
  upstream.on('stream', (stream, headers) => {
    const chunks = [];
    stream.on('data', chunk => chunks.push(chunk));
    stream.once('end', () => respond(stream, headers, decryptProto(Buffer.concat(chunks), 'fixture-only')));
  });
  upstream.listen(0, '127.0.0.1');
  await once(upstream, 'listening');
  const session = {
    region: 'jp', origin: `http://127.0.0.1:${upstream.address().port}`, bundle: 'test', lang: 'jpn',
    appVersion: 'test', apiSecret: 'fixture-only', credential: 'persistent', token: 'cached', master: 'cached',
    lastTicks: 0n, catalog: new Map(), bindingsValid: true, blocked: new Set(),
  };
  t.after(async () => {
    session.client?.destroy();
    await new Promise(resolve => upstream.close(resolve));
  });
  return session;
}

function send(stream, proto, headers = success) {
  stream.respond(headers);
  stream.end(encryptProto(proto, 'fixture-only'));
}

test('only distinct grade RPCs started at or after the threshold can be final', async t => {
  const boundary = Date.parse(borderRequest.final_after);
  let now = boundary - 1;
  t.mock.method(Date, 'now', () => now);
  const paths = [];
  let catalog = catalogProto;
  const session = await localRegion(t, (stream, headers) => {
    const path = headers[':path'];
    paths.push(path);
    if (path === gradePath) now = boundary + 100;
    send(stream, path === catalogPath ? catalog : gradeProto);
  });
  const signal = new AbortController().signal;
  const border = () => collect(session, borderRequest, signal);
  assert.equal((await border()).border.final, false);
  await collect(session, catalogRequest, signal);
  await assert.rejects(collect(session, { ...borderRequest, event_id: 'other' }, signal), /chapter_unavailable/);
  now = boundary - 1;
  const spanning = (await border()).border;
  assert.equal(spanning.started_at, new Date(boundary - 1).toISOString());
  assert.equal(spanning.observed_at, new Date(boundary + 100).toISOString());
  assert.equal(spanning.final, false);
  assert.equal((await border()).border.final, true);
  assert.equal(gradeObservation(boundary, boundary, borderRequest.final_after).final, true);
  assert.equal(gradeObservation(boundary, boundary + 1, '2026-11-20T00:00:00.000001Z').final, false);
  catalog = Buffer.concat([catalogProto, eventOf('other', chapterOf('c', 200)),
    eventOf('third', chapterOf('d', 200)), eventOf('fourth', chapterOf('d', 200))]);
  assert.deepEqual((await collect(session, catalogRequest, signal)).conflicts, ['c', 'd']);
  catalog = Buffer.alloc(0);
  assert.deepEqual((await collect(session, catalogRequest, signal)).conflicts, ['c', 'd']);
  await assert.rejects(border, /chapter_unavailable/);
  await collect(session, { ...catalogRequest, operation: 'ack', conflicts: ['c', 'd'] }, signal);
  assert.deepEqual((await collect(session, catalogRequest, signal)).conflicts, []);
  assert.deepEqual(paths, [gradePath, catalogPath, gradePath, gradePath, catalogPath, catalogPath, catalogPath]);
});

test('ineligible ownership changes quarantine a cached chapter binding', async t => {
  let catalog = catalogProto;
  const session = await localRegion(t, (stream, headers) => {
    send(stream, headers[':path'] === catalogPath ? catalog : gradeProto);
  });
  const signal = new AbortController().signal;
  await collect(session, catalogRequest, signal);
  catalog = eventOf('other', Buffer.concat([integer(5, 1), chapterOf('c', 200)]));

  assert.deepEqual((await collect(session, catalogRequest, signal)).conflicts, ['c']);
  await assert.rejects(collect(session, borderRequest, signal), /chapter_unavailable/);
});

test('invalid parents conflict and restart-time ineligible owners reject stale fallback', async t => {
  const signal = new AbortController().signal;
  const invalidCatalog = eventOf('bad.id', chapterOf('c', 200));
  const invalidSession = await localRegion(t, stream => send(stream, invalidCatalog));
  assert.deepEqual((await collect(invalidSession, catalogRequest, signal)).conflicts, ['c']);

  const ineligibleCatalog = eventOf('other', Buffer.concat([integer(5, 1), chapterOf('c', 200)]));
  const ineligibleSession = await localRegion(t, stream => send(stream, ineligibleCatalog));
  const snapshot = await collect(ineligibleSession, catalogRequest, signal);
  assert.deepEqual(snapshot.bindings, []);
  assert.deepEqual(snapshot.conflicts, ['c']);
  await assert.rejects(collect(ineligibleSession, borderRequest, signal), /chapter_unavailable/);
});

test('maximum catalog snapshot fits the bounded helper response', async t => {
  const id = (prefix, index) => `${prefix}${String(index).padStart(3, '0')}${'x'.repeat(92)}`;
  const catalog = Buffer.concat(Array.from({ length: 128 }, (_, index) =>
    eventOf(id('e', index), chapterOf(id('c', index), 200))));
  const session = await localRegion(t, stream => send(stream, catalog));
  const snapshot = await collect(session, catalogRequest, new AbortController().signal);
  const size = Buffer.byteLength(JSON.stringify(snapshot));

  assert.equal(snapshot.chapters.length, 128);
  assert(size > 65536);
  assert(size <= 131072);
});

test('quarantines replay in bounded acknowledged batches', async t => {
  const id = index => `c${String(index).padStart(3, '0')}${'x'.repeat(92)}`;
  let catalog = Buffer.concat(Array.from({ length: 128 }, (_, index) =>
    eventOf(`e${index}`, Buffer.concat([integer(5, 1), chapterOf(id(index), 200)]))));
  const session = await localRegion(t, stream => send(stream, catalog));
  const signal = new AbortController().signal;
  const first = await collect(session, catalogRequest, signal);
  assert.equal(first.conflicts.length, 128);
  assert.equal(first.quarantine_pending, false);

  catalog = eventOf('new', Buffer.concat([integer(5, 1), chapterOf('new_chapter', 200)]));
  const second = await collect(session, catalogRequest, signal);
  assert.equal(second.conflicts.length, 128);
  assert.equal(second.quarantine_pending, true);
  await collect(session, { ...catalogRequest, operation: 'ack', conflicts: second.conflicts }, signal);

  const last = await collect(session, catalogRequest, signal);
  assert.deepEqual(last.conflicts, ['new_chapter']);
  assert.equal(last.quarantine_pending, false);
});

for (const rejection of [{ ':status': 401 }, { ...success, 'grpc-status': '16' }]) {
  test(`authorization rejection ${rejection['grpc-status'] ?? 401} refreshes once from persistent credentials`, async t => {
    let rejectAll = false;
    const paths = [];
    const session = await localRegion(t, (stream, headers, proto) => {
      const path = headers[':path'];
      paths.push(path);
      assert.equal(headers['grpc-accept-encoding'], 'identity');
      if (path === loginPath) {
        assert.deepEqual(proto, text(1, 'persistent'));
        assert.equal(headers['x-app-auth-token'], undefined);
        send(stream, text(1, 'fresh-token'));
      } else if (path === masterPath) {
        send(stream, text(1, 'fresh-master'));
      } else {
        assert.equal(path, catalogPath);
        if (paths.length === 1 || rejectAll) send(stream, Buffer.alloc(0), rejection);
        else {
          assert.equal(headers['x-app-auth-token'], 'fresh-token');
          assert.equal(headers['x-app-master-version'], 'fresh-master');
          send(stream, catalogProto);
        }
      }
    });
    const signal = new AbortController().signal;
    const fetch = () => collect(session, catalogRequest, signal);
    assert.equal((await fetch()).chapters[0].event_id, 'e');
    assert.deepEqual(paths, [catalogPath, loginPath, masterPath, catalogPath]);
    paths.length = 0;
    rejectAll = true;
    await assert.rejects(fetch, error => rejection[':status'] === 401 ? error.httpStatus === 401 : error.grpcStatus === 16);
    assert.deepEqual(paths, [catalogPath, loginPath, masterPath, catalogPath]);
    assert.equal(session.token, '');
    assert.equal(session.master, '');
  });
}

test('nonzero integrity requirements fail closed before master and collection', async t => {
  const paths = [];
  const session = await localRegion(t, (stream, headers) => {
    paths.push(headers[':path']);
    send(stream, Buffer.concat([text(1, 'token'), integer(2, 2)]));
  });
  session.token = '';
  session.master = '';

  await assert.rejects(collect(session, catalogRequest, new AbortController().signal), /authentication/);
  assert.deepEqual(paths, [loginPath]);
});

test('missing, noncanonical grpc-status and wrong content type reject an otherwise valid body', async t => {
  let headers;
  const session = await localRegion(t, stream => send(stream, catalogProto, headers));
  const fetch = () => collect(session, catalogRequest, new AbortController().signal);
  for (const value of [undefined, '', ' ', '\t', ' 0', '0 ', '00', '+0', 'bad', 0, ['0']]) {
    assert.throws(() => checkGrpcStatus({}, { 'grpc-status': value }));
    assert.throws(() => checkGrpcStatus({ 'grpc-status': value }, {}));
  }
  assert.doesNotThrow(() => checkGrpcStatus({}, { 'grpc-status': '0' }));
  assert.throws(() => checkGrpcStatus({ 'grpc-status': '0' }, { 'grpc-status': '0' }));
  for (const status of [undefined, '', '00', '+0', 'bad']) {
    headers = { ...success, 'grpc-status': status };
    if (status === undefined) delete headers['grpc-status'];
    await assert.rejects(fetch);
  }
  headers = { ...success, 'content-type': 'application/octet-stream' };
  await assert.rejects(fetch, /protocol/);
});

test('aborting collection cancels the active HTTP/2 request without retrying', async t => {
  const arrived = Promise.withResolvers();
  const closed = Promise.withResolvers();
  const session = await localRegion(t, stream => {
    stream.once('close', () => closed.resolve(stream.rstCode));
    arrived.resolve();
  });
  const operation = new AbortController();
  const pending = collect(session, catalogRequest, operation.signal);
  await arrived.promise;
  operation.abort();
  await assert.rejects(pending, /transport/);
  assert.equal(await closed.promise, http2.constants.NGHTTP2_CANCEL);
});

function post(socketPath, path, input, signal) {
  const body = JSON.stringify(input);
  return new Promise((resolve, reject) => {
    const req = request({
      socketPath, path, method: 'POST', agent: false, signal,
      headers: { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) },
    }, res => {
      let data = '';
      res.setEncoding('utf8');
      res.on('data', chunk => { data += chunk; });
      res.on('error', reject);
      res.on('end', () => resolve({ code: res.statusCode, data }));
    });
    req.on('error', reject);
    req.end(body);
  });
}

test('UDS shares one eight-second budget across login and master and cancels on disconnect', { timeout: 10000 }, async t => {
  let arrival = Promise.withResolvers();
  const region = await localRegion(t, (stream, headers) => arrival.resolve({ stream, path: headers[':path'] }));
  const connect = http2.connect;
  t.mock.method(http2, 'connect', () => (region.client = connect(region.origin)));
  const dir = await mkdtemp(join(tmpdir(), 'grade-'));
  const socket = join(dir, 'rpc.sock');
  const helper = createGradeServer();
  helper.listen(socket);
  await once(helper, 'listening');
  t.after(async () => {
    helper.closeAllConnections();
    await new Promise(resolve => helper.close(resolve));
    await rm(dir, { recursive: true });
  });
  assert.equal((await post(socket, '/v1/bootstrap', config())).code, 200);
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const timed = post(socket, '/v1/collect', catalogRequest);
  const login = await arrival.promise;
  assert.equal(login.path, loginPath);
  t.mock.timers.tick(7500);
  arrival = Promise.withResolvers();
  send(login.stream, text(1, 'token'));
  const master = await arrival.promise;
  assert.equal(master.path, masterPath);
  const closed = once(master.stream, 'close');
  assert.equal((await post(socket, '/v1/collect', catalogRequest)).code, 429);
  t.mock.timers.tick(500);
  assert.deepEqual(await timed, { code: 502, data: '{"version":1,"status":"upstream_transport"}' });
  await closed;
  assert.equal(master.stream.rstCode, http2.constants.NGHTTP2_CANCEL);

  const caller = new AbortController();
  arrival = Promise.withResolvers();
  const dropped = post(socket, '/v1/collect', catalogRequest, caller.signal);
  const next = await arrival.promise;
  assert.equal(next.path, loginPath);
  const reset = once(next.stream, 'close');
  caller.abort();
  await assert.rejects(dropped, { name: 'AbortError' });
  await reset;
  assert.equal(next.stream.rstCode, http2.constants.NGHTTP2_CANCEL);

  arrival = Promise.withResolvers();
  const denied = post(socket, '/v1/collect', catalogRequest);
  const last = await arrival.promise;
  last.stream.respond({ ':status': 403 });
  last.stream.end('private upstream body');
  assert.deepEqual(await denied, { code: 502, data: '{"version":1,"status":"upstream_http_403"}' });
});
