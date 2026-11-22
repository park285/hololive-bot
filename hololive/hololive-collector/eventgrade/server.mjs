import { createCipheriv, createDecipheriv, createHash } from 'node:crypto';
import { chmodSync } from 'node:fs';
import http2 from 'node:http2';
import { createServer } from 'node:http';
import { isAbsolute } from 'node:path';
import { gunzipSync, gzipSync } from 'node:zlib';

const limit = 16384;
const upstreamLimit = 2 * 1024 * 1024;
const collectTimeoutMs = 8000;
const regions = new Set(['jp', 'us', 'as']);
const profiles = {
  jp: { origin: 'https://jp.game-hololive-dreams.com', bundle: 'game.qualiarts.hololive.dreams.jp', lang: 'jpn' },
  us: { origin: 'https://us.game-hololive-dreams.com', bundle: 'game.qualiarts.hololive.dreams.com', lang: 'eng' },
  as: { origin: 'https://as.game-hololive-dreams.com', bundle: 'game.qualiarts.hololive.dreams.com', lang: 'eng' },
};
const epochTicks = 621355968000000000n;
const keys = (value, names) => value !== null && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === names.length && names.every(k => Object.hasOwn(value, k));
const secret = (s, n) => typeof s === 'string' && s.length > 0 && s.length <= n && !/[^!-~]/u.test(s);

export function validBootstrap(input) {
  if (!keys(input, ['version', 'secret']) || input.version !== 1 || !keys(input.secret, ['app_version', 'api_secret', 'credentials'])) return false;
  const { app_version, api_secret, credentials } = input.secret;
  return secret(app_version, 128) && secret(api_secret, 512) && keys(credentials, [...regions]) &&
    Object.values(credentials).every(value => secret(value, 2048)) && new Set(Object.values(credentials)).size === 3;
}

export function parseRequest(raw) {
  if (raw.length > limit) throw new Error('size');
  const str = new TextDecoder('utf-8', { fatal: true }).decode(raw);
  const value = JSON.parse(str);
  if (JSON.stringify(value) !== str) throw new Error('noncanonical');
  return value;
}


export function validCollect(input) {
  if (!regions.has(input?.region) || input.version !== 1) return false;
  if (input.operation === 'catalog') return keys(input, ['version', 'region', 'operation']);
  if (input.operation === 'ack') return keys(input, ['version', 'region', 'operation', 'conflicts']) &&
    Array.isArray(input.conflicts) && input.conflicts.length <= 128 && new Set(input.conflicts).size === input.conflicts.length &&
    input.conflicts.every(id => typeof id === 'string' && /^[A-Za-z0-9_-]{1,96}$/u.test(id));
  return input.operation === 'border' && keys(input, ['version', 'region', 'operation', 'event_id', 'chapter_id', 'final_after', 'binding_observed_at']) &&
    [input.event_id, input.chapter_id].every(id => typeof id === 'string' && /^[A-Za-z0-9_-]{1,96}$/u.test(id)) &&
    [input.final_after, input.binding_observed_at].every(value => typeof value === 'string' && Number.isFinite(Date.parse(value)));
}

const grpcType = 'application/grpc+proto-enc';
const grpcPrefixSize = 5;
const saltOffset = grpcPrefixSize + 4;

function transformBlocks(factory, secret, salt, input) {
  const digest = createHash('md5').update(secret, 'utf8');
  const aes = factory('aes-128-cbc', digest.copy().digest(), digest.update(salt).digest());
  return Buffer.concat([aes.update(input), aes.final()]);
}

export function encryptProto(proto, apiSecret, ticks = BigInt(Date.now()) * 10000n + epochTicks) {
  if (!proto.length) return Buffer.alloc(grpcPrefixSize);
  const prefix = Buffer.alloc(saltOffset + 8);
  prefix.writeBigUInt64LE(ticks, saltOffset);
  prefix.writeUInt16LE(10, grpcPrefixSize);
  prefix[grpcPrefixSize + 2] = Number(proto.length > 2048);
  prefix[grpcPrefixSize + 3] = 8;
  const blocks = transformBlocks(createCipheriv, apiSecret, prefix.subarray(saltOffset),
    prefix[grpcPrefixSize + 2] ? gzipSync(proto) : proto);
  prefix.writeUInt32BE(prefix.length - grpcPrefixSize + blocks.length, 1);
  return Buffer.concat([prefix, blocks]);
}

export function decryptProto(frame, apiSecret) {
  if (frame.length < grpcPrefixSize || frame.length > upstreamLimit || frame[0] !== 0 ||
    frame.readUInt32BE(1) !== frame.length - grpcPrefixSize) throw new Error('grpc_frame');
  if (frame.length === grpcPrefixSize) return frame.subarray(grpcPrefixSize);
  if (frame.length < saltOffset) throw new Error('encrypted_header');
  const mode = frame[grpcPrefixSize + 2];
  const dataOffset = saltOffset + frame[grpcPrefixSize + 3];
  if (mode > 1 || dataOffset !== grpcPrefixSize + 2 + frame.readUInt16LE(grpcPrefixSize) ||
    dataOffset >= frame.length || (frame.length - dataOffset) % 16) throw new Error('encrypted_boundary');
  const data = transformBlocks(createDecipheriv, apiSecret, frame.subarray(saltOffset, dataOffset), frame.subarray(dataOffset));
  return mode === 0 ? data : gunzipSync(data, { maxOutputLength: upstreamLimit });
}

function encodeVarint(value) {
  const output = [];
  let current = BigInt(value);
  while (current >= 128n) {
    output.push(Number(current & 127n) | 128);
    current >>= 7n;
  }
  output.push(Number(current));
  return Buffer.from(output);
}

function encodeStringField(tag, value) {
  const data = Buffer.from(value, 'utf8');
  return Buffer.concat([encodeVarint(BigInt(tag << 3 | 2)), encodeVarint(data.length), data]);
}

function readVarint(buffer, offset) {
  let value = 0n;
  let shift = 0n;
  for (let i = offset; i < buffer.length && i < offset + 10; i++) {
    const byte = buffer[i];
    if (i === offset + 9 && byte > 1) throw new Error('protobuf_varint');
    value |= BigInt(byte & 127) << shift;
    if ((byte & 128) === 0) return [value, i + 1];
    shift += 7n;
  }
  throw new Error('protobuf_varint');
}

const scalarInt = 0;
const scalarBytes = 1;
const catalogSchema = { 1: 128 };
const eventSchema = { 1: scalarBytes, 2: scalarInt, 4: scalarInt, 5: scalarInt, 14: scalarBytes };
const marathonSchema = { 3: scalarInt, 4: 128, 5: scalarInt };
const chapterSchema = { 1: scalarBytes, 4: scalarInt, 15: scalarInt };
const gradeSchema = { 1: scalarBytes };
const rankingSchema = { 4: 32 };
const cutoffSchema = { 1: scalarInt, 2: scalarInt };
const loginSchema = { 1: scalarBytes, 2: scalarInt };
const masterSchema = { 1: scalarBytes };

function decodeFields(buffer, schema) {
  if (buffer.length > upstreamLimit) throw new Error('protobuf_limit');
  const result = new Map();
  let offset = 0;
  let count = 0;
  while (offset < buffer.length) {
    if (++count > 1024) throw new Error('protobuf_limit');
    let key;
    [key, offset] = readVarint(buffer, offset);
    const tag = Number(key >> 3n);
    const wire = Number(key & 7n);
    if (tag < 1 || key > 0xffffffffn) throw new Error('protobuf_tag');
    const rule = schema[tag];
    if (rule !== undefined && wire !== (rule === scalarInt ? 0 : 2)) throw new Error('protobuf_type');
    let value;
    if (wire === 0) [value, offset] = readVarint(buffer, offset);
    else if (wire === 2) {
      let length;
      [length, offset] = readVarint(buffer, offset);
      if (length > BigInt(buffer.length - offset)) throw new Error('protobuf_length');
      const end = offset + Number(length);
      if (rule !== undefined) value = buffer.subarray(offset, end);
      offset = end;
    } else if (wire === 1) {
      if (offset + 8 > buffer.length) throw new Error('protobuf_fixed64');
      offset += 8;
    } else if (wire === 5) {
      if (offset + 4 > buffer.length) throw new Error('protobuf_fixed32');
      offset += 4;
    } else throw new Error('protobuf_wire');
    if (rule === undefined) continue;
    if (rule <= scalarBytes) {
      if (result.has(tag)) throw new Error('protobuf_duplicate');
      result.set(tag, value);
    } else {
      const records = result.get(tag);
      if (!records) result.set(tag, [value]);
      else {
        if (records.length >= rule) throw new Error('protobuf_limit');
        records.push(value);
      }
    }
  }
  return result;
}

const utf8 = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

function string(fields, tag) {
  const value = fields.get(tag);
  return value === undefined ? '' : utf8.decode(value);
}

function integer(fields, tag) {
  return fields.get(tag) ?? 0n;
}

function timestamp(value) {
  const milliseconds = value < 100000000000n ? value * 1000n : value;
  if (milliseconds < 0n || milliseconds > 8640000000000000n) throw new Error('timestamp');
  return new Date(Number(milliseconds));
}

function parseEventCatalogSnapshot(proto) {
  const chapters = [];
  const owners = new Map();
  const conflicts = new Set();
  let chapterCount = 0;
  for (const eventRaw of decodeFields(proto, catalogSchema).get(1) ?? []) {
    const event = decodeFields(eventRaw, eventSchema);
    const eventID = string(event, 1);
    const eventType = Number(integer(event, 2));
    const startValue = integer(event, 4);
    const startsAt = timestamp(startValue);
    const eventEndsAt = timestamp(integer(event, 5));
    const marathonRaw = event.get(14);
    if (!marathonRaw) continue;
    const marathon = decodeFields(marathonRaw, marathonSchema);
    const eventIdentityValid = /^[A-Za-z0-9_-]{1,96}$/u.test(eventID);
    const eligible = eventIdentityValid && [1, 2].includes(eventType) &&
      startValue > 0n && startsAt < eventEndsAt && integer(marathon, 5) !== 1n;
    let previousEnd = startsAt;
    for (const chapterRaw of marathon.get(4) ?? []) {
      if (++chapterCount > 128) throw new Error('catalog_limit');
      const chapter = decodeFields(chapterRaw, chapterSchema);
      const chapterID = string(chapter, 1);
      const chapterIdentityValid = /^[A-Za-z0-9_-]{1,96}$/u.test(chapterID);
      if (chapterIdentityValid) {
        if (!eventIdentityValid || owners.has(chapterID)) conflicts.add(chapterID);
        else owners.set(chapterID, eventID);
      }
      if (!eligible) continue;
      const endsAt = timestamp(integer(chapter, 4));
      const revealValue = integer(chapter, 15) || integer(marathon, 3);
      const revealsAt = timestamp(revealValue);
      if (!chapterIdentityValid || previousEnd >= endsAt || endsAt > eventEndsAt || revealsAt < endsAt) throw new Error('chapter_contract');
      chapters.push({
        event_id: eventID,
        chapter_id: chapterID,
        event_type: eventType === 1 ? 'normal' : 'relay',
        starts_at: previousEnd.toISOString(),
        ends_at: endsAt.toISOString(),
        event_ends_at: eventEndsAt.toISOString(),
        reveals_at: revealsAt.toISOString(),
      });
      previousEnd = endsAt;
    }
  }
  return {
    chapters: chapters.filter(chapter => !conflicts.has(chapter.chapter_id)),
    bindings: [...owners].filter(([chapterID]) => !conflicts.has(chapterID))
      .map(([chapter_id, event_id]) => ({ event_id, chapter_id })),
    conflicts: [...conflicts],
  };
}

export function parseEventCatalog(proto) {
  return parseEventCatalogSnapshot(proto).chapters;
}

export function parseGradeRows(proto) {
  const resultRaw = decodeFields(proto, gradeSchema).get(1);
  if (!resultRaw) throw new Error('grade_result');
  const rows = [];
  for (const rankRaw of decodeFields(resultRaw, rankingSchema).get(4) ?? []) {
    const rankInfo = decodeFields(rankRaw, cutoffSchema);
    const rank = integer(rankInfo, 1);
    const score = integer(rankInfo, 2);
    if (rank < 1n || rank > 2147483647n || score < 0n || score > 9223372036854775807n) throw new Error('grade_range');
    rows.push({ rank: Number(rank), score: score.toString() });
  }
  return rows;
}

export function validateGradeRows(rows) {
  if (rows.length === 0 || rows.length > 32) throw new Error('grade_cutoffs');
  rows.sort((left, right) => left.rank - right.rank);
  for (let index = 1; index < rows.length; index++) {
    if (rows[index - 1].rank === rows[index].rank || BigInt(rows[index - 1].score) < BigInt(rows[index].score)) throw new Error('grade_cutoffs');
  }
  return rows;
}

class UpstreamError extends Error {
  constructor(kind, { httpStatus, grpcStatus } = {}) {
    super(kind);
    this.httpStatus = httpStatus;
    this.grpcStatus = grpcStatus;
  }
}

function sessionFor(region, config) {
  const profile = profiles[region];
  return {
    region,
    origin: profile.origin,
    bundle: profile.bundle,
    lang: profile.lang,
    appVersion: config.app_version,
    apiSecret: config.api_secret,
    credential: config.credentials[region],
    token: '',
    master: '',
    catalog: new Map(),
    bindingsValid: true,
    blocked: new Set(),
    lastTicks: 0n,
    client: undefined,
  };
}

function requestID(session) {
  const now = BigInt(Date.now()) * 10000n + epochTicks;
  session.lastTicks = now > session.lastTicks ? now : session.lastTicks + 1n;
  return session.lastTicks.toString();
}

function clientFor(session) {
  if (session.client && !session.client.closed && !session.client.destroyed) return session.client;
  const client = http2.connect(session.origin);
  client.setTimeout(15000, () => client.destroy());
  client.on('error', () => {});
  session.client = client;
  return client;
}

function checkResponseHeaders(headers) {
  const status = headers[':status'];
  if (status !== 200) throw new UpstreamError('http', {
    httpStatus: Number.isInteger(status) && status >= 100 && status <= 599 ? status : undefined,
  });
  if (headers['content-type'] !== grpcType) throw new UpstreamError('protocol');
}

export function checkGrpcStatus(headers, trailers) {
  const inHeaders = headers['grpc-status'];
  const inTrailers = trailers['grpc-status'];
  if (inHeaders !== undefined && inTrailers !== undefined) throw new UpstreamError('protocol');
  const status = inTrailers ?? inHeaders;
  if (status !== '0') throw new UpstreamError('grpc', { grpcStatus: status === '16' ? 16 : undefined });
}

async function call(session, path, proto, authenticated, signal) {
  signal.throwIfAborted();
  const headers = {
    ':method': 'POST',
    ':path': path,
    'content-type': grpcType,
    te: 'trailers',
    'grpc-accept-encoding': 'identity',
    'x-app-version': session.appVersion,
    'x-app-bundle-id': session.bundle,
    'x-app-lang-type': session.lang,
    'x-app-os-type': 'Android',
    'x-app-store-type': 'GooglePlay',
  };
  if (authenticated) {
    if (!session.token || !session.master) throw new UpstreamError('session');
    headers['x-app-auth-token'] = session.token;
    headers['x-app-master-version'] = session.master;
    headers['x-app-request-id'] = requestID(session);
  }
  const body = encryptProto(proto, session.apiSecret);
  const client = clientFor(session);
  return new Promise((resolve, reject) => {
    const startedAt = Date.now();
    const stream = client.request(headers, { signal });
    const chunks = [];
    let size = 0;
    let responseHeaders = {};
    let trailers = {};
    let failure;
    let ended = false;
    stream.on('response', value => {
      responseHeaders = value;
      try {
        checkResponseHeaders(value);
      } catch (error) {
        failure = error;
        stream.close(http2.constants.NGHTTP2_CANCEL);
      }
    });
    stream.on('trailers', value => { trailers = value; });
    stream.on('data', chunk => {
      if (failure) return;
      size += chunk.length;
      if (size > upstreamLimit) {
        failure = new UpstreamError('response_limit');
        stream.close(http2.constants.NGHTTP2_CANCEL);
      } else chunks.push(chunk);
    });
    stream.on('error', () => { failure ??= new UpstreamError('transport'); });
    stream.on('end', () => { ended = true; });
    stream.once('close', () => {
      if (signal.aborted) return reject(new UpstreamError('transport'));
      if (failure) return reject(failure);
      if (!ended || stream.rstCode !== 0) return reject(new UpstreamError('transport'));
      try {
        checkGrpcStatus(responseHeaders, trailers);
        resolve({ startedAt, proto: decryptProto(Buffer.concat(chunks, size), session.apiSecret) });
      } catch (error) {
        reject(error instanceof UpstreamError ? error : new UpstreamError('protocol'));
      }
    });
    stream.end(body);
  });
}

async function authenticate(session, signal) {
  if (session.token && session.master) return;
  const response = await call(session, '/rpc.api.Auth/Login', encodeStringField(1, session.credential), false, signal);
  const login = decodeFields(response.proto, loginSchema);
  const token = string(login, 1);
  if (!secret(token, 8192) || integer(login, 2) !== 0n) throw new UpstreamError('authentication');
  const version = await call(session, '/rpc.api.Master/Get', Buffer.alloc(0), false, signal);
  const master = string(decodeFields(version.proto, masterSchema), 1);
  if (!secret(master, 512)) throw new UpstreamError('master');
  session.token = token;
  session.master = master;
}

async function authenticatedCall(session, path, proto, signal) {
  for (let attempt = 0; ; attempt++) {
    try {
      await authenticate(session, signal);
      return await call(session, path, proto, true, signal);
    } catch (error) {
      if (!(error instanceof UpstreamError) || (error.httpStatus !== 401 && error.grpcStatus !== 16)) throw error;
      session.token = '';
      session.master = '';
      if (attempt !== 0 || signal.aborted) throw error;
    }
  }
}

export function gradeObservation(startedAt, completedAt, finalAfter) {
  const subMillisecond = /\.\d{3}(\d+)/u.exec(finalAfter)?.[1];
  const threshold = Date.parse(finalAfter) + Number(/[1-9]/u.test(subMillisecond ?? ''));
  return { started_at: new Date(startedAt).toISOString(), observed_at: new Date(completedAt).toISOString(), final: startedAt >= threshold };
}

export async function collect(session, input, signal) {
  if (input.operation === 'ack') {
    for (const chapterID of input.conflicts) session.blocked.delete(chapterID);
    return { version: 1, status: 'acknowledged' };
  }
  if (input.operation === 'catalog') {
    const { proto } = await authenticatedCall(session, '/rpc.api.Event/ListEventInfo', Buffer.alloc(0), signal);
    let snapshot;
    try {
      snapshot = parseEventCatalogSnapshot(proto);
    } catch (error) {
      session.catalog = new Map();
      session.bindingsValid = false;
      throw error;
    }
    const conflicts = new Set(snapshot.conflicts);
    const eligible = new Set(snapshot.chapters.map(chapter => chapter.chapter_id));
    for (const binding of snapshot.bindings) {
      if (!eligible.has(binding.chapter_id)) conflicts.add(binding.chapter_id);
    }
    for (const binding of snapshot.bindings) {
      const stored = session.catalog.get(binding.chapter_id);
      if (stored && stored.event_id !== binding.event_id) conflicts.add(binding.chapter_id);
    }
    for (const chapterID of conflicts) {
      session.catalog.delete(chapterID);
      session.blocked.add(chapterID);
    }
    const chapters = snapshot.chapters.filter(chapter => !session.blocked.has(chapter.chapter_id));
    const bindings = snapshot.bindings.filter(binding => !session.blocked.has(binding.chapter_id));
    for (const binding of bindings) session.catalog.set(binding.chapter_id, binding);
    for (const chapter of chapters) {
      session.catalog.set(chapter.chapter_id, chapter);
    }
    session.bindingsValid = true;
    const quarantines = [...session.blocked].slice(0, 128);
    return {
      version: 1,
      region: session.region,
      chapters,
      bindings,
      conflicts: quarantines,
      quarantine_pending: session.blocked.size > quarantines.length,
    };
  }
  if (!session.bindingsValid) throw new UpstreamError('chapter_unavailable');
  if (session.blocked.has(input.chapter_id)) throw new UpstreamError('chapter_unavailable');
  let chapter = session.catalog.get(input.chapter_id);
  if (chapter && chapter.event_id !== input.event_id) throw new UpstreamError('chapter_unavailable');
  if (!chapter) {
    const observedAt = Date.parse(input.binding_observed_at);
    if (observedAt > Date.now()) throw new UpstreamError('chapter_unavailable');
    chapter = { event_id: input.event_id, chapter_id: input.chapter_id };
    session.catalog.set(chapter.chapter_id, chapter);
  }
  const { proto, startedAt } = await authenticatedCall(session, '/rpc.api.Marathon/ListMarathonScoreRankingGrade',
    encodeStringField(1, chapter.chapter_id), signal);
  return {
    version: 1,
    border: {
      region: session.region,
      event_id: chapter.event_id,
      chapter_id: chapter.chapter_id,
      board_kind: 'marathon_score',
      ...gradeObservation(startedAt, Date.now(), input.final_after),
      ranks: validateGradeRows(parseGradeRows(proto)),
    },
  };
}

export function createGradeServer() {
  let busy = false;
  let sessions;
  const server = createServer({ maxHeaderSize: 2048, requestTimeout: 12000, headersTimeout: 2000, keepAliveTimeout: 1000 }, async (req, res) => {
    const reply = (code, body) => {
      if (res.destroyed) return;
      res.writeHead(code, { 'content-type': 'application/json', 'cache-control': 'no-store' });
      res.end(JSON.stringify(body));
    };
    if (busy) return reply(429, { version: 1, status: 'busy' });
    if (req.method !== 'POST' || !['/v1/bootstrap', '/v1/collect'].includes(req.url) || req.headers['content-type'] !== 'application/json') return reply(400, { version: 1, status: 'invalid_request' });
    const size = Number(req.headers['content-length']);
    if (!Number.isInteger(size) || size < 1 || size > limit) return reply(413, { version: 1, status: 'invalid_size' });
    busy = true;
    const operation = new AbortController();
    const disconnect = () => {
      operation.abort();
      if (!req.complete) req.destroy();
    };
    res.once('close', disconnect);
    const timer = setTimeout(disconnect, collectTimeoutMs);
    const deadline = performance.now() + collectTimeoutMs;
    try {
      const chunks = [];
      let bytesRead = 0;
      for await (const chunk of req) {
        bytesRead += chunk.length;
        if (bytesRead > size) return reply(413, { version: 1, status: 'invalid_size' });
        chunks.push(chunk);
      }
      if (bytesRead !== size) return reply(400, { version: 1, status: 'invalid_size' });
      operation.signal.throwIfAborted();
      const input = parseRequest(Buffer.concat(chunks, bytesRead));
      if (req.url === '/v1/bootstrap') {
        if (sessions || !validBootstrap(input)) return reply(400, { version: 1, status: 'invalid_bootstrap' });
        sessions = new Map([...regions].map(region => [region, sessionFor(region, input.secret)]));
        return reply(200, { version: 1, status: 'ready' });
      }
      if (!sessions || !validCollect(input)) return reply(400, { version: 1, status: 'invalid_request' });
      try {
        const result = await collect(sessions.get(input.region), input, operation.signal);
        if (operation.signal.aborted || performance.now() >= deadline) throw new UpstreamError('transport');
        return reply(200, result);
      } catch (error) {
        const allowed = ['transport', 'response_limit', 'http', 'grpc', 'protocol', 'session', 'authentication', 'master',
          'timestamp', 'chapter_contract', 'chapter_ambiguous', 'chapter_unavailable', 'catalog_limit', 'grade_result', 'grade_range', 'grade_cutoffs'];
        const kind = error instanceof Error && allowed.includes(error.message) ? error.message : 'protocol';
        const status = kind === 'http' && error instanceof UpstreamError && error.httpStatus ? `http_${error.httpStatus}` : kind;
        return reply(502, { version: 1, status: `upstream_${status}` });
      }
    } catch {
      return reply(400, { version: 1, status: 'invalid_request' });
    } finally {
      clearTimeout(timer);
      res.off('close', disconnect);
      busy = false;
    }
  });
  server.maxConnections = 2;
  server.setTimeout(13000, connection => connection.destroy());
  server.on('clientError', (_, connection) => connection.destroy());
  server.once('close', () => {
    if (sessions) {
      for (const session of sessions.values()) {
        session.credential = '';
        session.token = '';
        session.master = '';
        session.apiSecret = '';
        session.client?.destroy();
      }
    }
  });
  return server;
}

function serve(socket) {
  process.umask(0o177);
  const server = createGradeServer();
  server.on('error', () => process.exit(1));
  server.listen(socket, () => chmodSync(socket, 0o600));
  process.once('SIGTERM', () => {
    server.close();
    server.closeAllConnections();
  });
}

if (import.meta.main) {
  const socket = process.argv[2];
  if (process.versions.node !== '26.13.1' || process.argv.length !== 3 || !isAbsolute(socket)) process.exit(1);
  serve(socket);
}
