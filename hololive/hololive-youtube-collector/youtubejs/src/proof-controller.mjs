// @ts-check
import { parseChallengeData } from "bgutils-js/botguard";
import { getHeaders } from "bgutils-js/utils";
import { ProofBrokerClient, ProofError } from "./proof-broker.mjs";
import { runWithoutRequestContext } from "./request-context.mjs";

// upstream BgUtils의 공개 WEB 요청 식별자이며 계정/운영 credential이 아닙니다.
const requestKey = "O43z0dpjhgX20SCx4KAo";
const createURL = "https://jnn-pa.googleapis.com/$rpc/google.internal.waa.v1.Waa/Create";
const integrityURL = "https://www.youtube.com/api/jnn/v1/GenerateIT";
const bodyLimit = 512 * 1024;
const minimumAttemptInterval = 300_000;
const maximumLifetime = 43_200_000;
const expiryMargin = 30_000;

/** @typedef {{ generation: string, deadline: number, refreshAt: number, expiresAt: string }} ProofSession */

/** 네트워크는 신뢰된 helper가 소유하고 외부 interpreter는 별도 broker에만 전달합니다. */
export class ProofController {
  /**
   * @param {{
   *   fetchImpl: import("./upstream-feeds.d.ts").InnertubeFetch,
   *   userAgent: string,
   *   broker?: ProofBrokerClient,
   *   clock?: () => number,
   *   wallClock?: () => number,
   * }} options
   */
  constructor(options) {
    if (typeof options.fetchImpl !== "function" || !text(options.userAgent, 1_024)) {
      throw new ProofError("proof_configuration");
    }
    this.fetch = options.fetchImpl;
    this.userAgent = options.userAgent;
    this.broker = options.broker ?? new ProofBrokerClient();
    this.clock = options.clock ?? (() => performance.now());
    this.wallClock = options.wallClock ?? Date.now;
    this.stop = new AbortController();
    /** @type {ProofSession | undefined} */
    this.session = undefined;
    /** @type {Promise<void> | undefined} */
    this.attempt = undefined;
    /** @type {Promise<void> | undefined} */
    this.closing = undefined;
    /** @type {string | undefined} */
    this.ownedGeneration = undefined;
    /** @type {import("./contracts.d.ts").ProofState} */
    this.state = "COLD";
    this.lastError = "";
    this.nextAttempt = 0;
    this.nextAttemptWall = 0;
    this.bootstrapAttempts = 0;
    this.bootstrapSuccesses = 0;
    this.upstreamRequests = 0;
    this.mintedTotal = 0;
    this.attachedTotal = 0;
  }

  /** @returns {import("./contracts.d.ts").ProofStatus} */
  status() {
    return {
      state: this.session && this.clock() >= this.session.deadline ? "EXPIRED" : this.state,
      ...(this.session ? { generation: this.session.generation, expires_at: this.session.expiresAt } : {}),
      ...(this.lastError ? { last_error: this.lastError } : {}),
      ...(this.nextAttemptWall ? { next_attempt_at: new Date(this.nextAttemptWall).toISOString() } : {}),
      bootstrap_attempts: this.bootstrapAttempts,
      bootstrap_successes: this.bootstrapSuccesses,
      upstream_requests: this.upstreamRequests,
      minted_total: this.mintedTotal,
      attached_total: this.attachedTotal,
    };
  }

  /**
   * 마지막 generation/만료 확인과 player 호출 사이에 다른 await를 두지 않습니다.
   * 발급 실패는 재시도가 아니라 기존 단일 무토큰 player 경로를 유지합니다.
   * @template T
   * @param {string} videoId
   * @param {(token?: string) => Promise<T>} send
   * @param {AbortSignal} [signal]
   * @returns {Promise<T>}
   */
  async runPlayer(videoId, send, signal) {
    signal?.throwIfAborted();
    this.stop.signal.throwIfAborted();
    this.ensureWarm();
    const session = this.session;
    let token;
    if (session && this.clock() < session.deadline) {
      const mintSignal = signal ? AbortSignal.any([signal, this.stop.signal]) : this.stop.signal;
      try {
        token = await this.broker.mint(session.generation, videoId, mintSignal);
        this.mintedTotal += 1;
      } catch (error) {
        // 개별 RPC 취소만으로 공유 세대를 버리지 않습니다. 실제 퇴장은 다음 broker 응답으로 판정합니다.
        // 옛 세대의 늦은 실패 역시 이미 준비된 새 세대를 무효화하지 않습니다.
        if (!signal?.aborted && !this.stop.signal.aborted && this.session === session) {
          this.session = undefined;
          this.state = "UNAVAILABLE";
          this.lastError = safeCode(error);
        }
      }
      signal?.throwIfAborted();
      this.stop.signal.throwIfAborted();
      if (this.session !== session || this.clock() >= session.deadline) token = undefined;
    }
    if (token !== undefined) this.attachedTotal += 1;
    return send(token);
  }

  ensureWarm() {
    const now = this.clock();
    if (this.stop.signal.aborted || this.attempt || now < this.nextAttempt ||
        (this.session && now < this.session.refreshAt)) return;
    this.session = undefined;
    this.state = "WARMING";
    this.lastError = "";
    this.nextAttempt = now + minimumAttemptInterval;
    this.nextAttemptWall = this.wallClock() + minimumAttemptInterval;
    this.bootstrapAttempts += 1;
    const signal = AbortSignal.any([this.stop.signal, AbortSignal.timeout(15_000)]);
    this.attempt = runWithoutRequestContext(async () => {
      try {
        const session = await this.issue(signal);
        signal.throwIfAborted();
        if (this.clock() >= session.deadline) throw new ProofError("proof_expired");
        this.session = session;
        this.state = "READY";
        this.bootstrapSuccesses += 1;
      } catch (error) {
        this.session = undefined;
        this.lastError = safeCode(error);
        this.state = this.stop.signal.aborted ? "STOPPED" : "UNAVAILABLE";
        await this.retireOwned();
      }
    }).finally(() => { this.attempt = undefined; });
  }

  /** @param {AbortSignal} signal @returns {Promise<ProofSession>} */
  async issue(signal) {
    const generation = await this.broker.freshGeneration(signal);
    this.ownedGeneration = generation;
    await this.broker.prepare(generation, this.userAgent, signal);
    signal.throwIfAborted();
    let calls = 0;
    const headers = { ...getHeaders(), "user-agent": this.userAgent };
    /** @param {string} url @param {unknown[] | undefined} payload */
    const upstream = async (url, payload) => {
      signal.throwIfAborted();
      if (++calls > 3) throw new ProofError("proof_request_budget");
      this.upstreamRequests += 1;
      const response = await this.fetch(url, {
        method: payload === undefined ? "GET" : "POST",
        headers,
        body: payload === undefined ? undefined : JSON.stringify(payload),
        redirect: "error", credentials: "omit", signal,
      });
      if (!response.ok) {
        await response.body?.cancel();
        throw new ProofError("attestation_http");
      }
      return readBounded(response, signal);
    };
    const raw = parseJSON(await upstream(createURL, [requestKey]));
    if (!Array.isArray(raw)) throw new ProofError("challenge_structure");
    let challenge;
    try {
      challenge = parseChallengeData(raw);
    } catch {
      throw new ProofError("challenge_structure");
    }
    if (!text(challenge.program, bodyLimit) || !text(challenge.globalName, 256)) {
      throw new ProofError("challenge_structure");
    }
    let interpreter = challenge.interpreterJavascript?.privateDoNotAccessOrElseSafeScriptWrappedValue;
    if (interpreter === undefined) {
      const resource = challenge.interpreterUrl?.privateDoNotAccessOrElseTrustedResourceUrlWrappedValue;
      if (!text(resource, 2_048)) throw new ProofError("challenge_structure");
      let url;
      try {
        url = new URL(resource.startsWith("//") ? `https:${resource}` : resource);
      } catch {
        throw new ProofError("interpreter_url");
      }
      if (url.protocol !== "https:" || url.hostname !== "www.google.com" || url.port ||
          url.username || url.password || url.search || url.hash || !/^\/js\/th\/[A-Za-z0-9_-]+\.js$/.test(url.pathname)) {
        throw new ProofError("interpreter_url");
      }
      interpreter = await upstream(url.href, undefined);
    }
    if (!text(interpreter, bodyLimit)) throw new ProofError("challenge_structure");
    const snapshot = await this.broker.challenge({
      generation, program: challenge.program, global_name: challenge.globalName, interpreter,
    }, signal);
    const requestedAt = this.clock();
    const requestedWall = this.wallClock();
    const integrity = parseJSON(await upstream(integrityURL, [requestKey, snapshot]));
    if (!Array.isArray(integrity) || !text(integrity[0], 8_192) ||
        !/^[A-Za-z0-9+/_-]+={0,2}$/.test(integrity[0]) || !Number.isSafeInteger(integrity[1]) || integrity[1] <= 0) {
      throw new ProofError("integrity_structure");
    }
    const lifetime = Math.min(integrity[1] * 1_000, maximumLifetime);
    const deadline = requestedAt + lifetime - expiryMargin;
    const validForMs = Math.floor(deadline - this.clock());
    if (validForMs <= 0) throw new ProofError("proof_expired");
    await this.broker.activate(generation, integrity[0], validForMs, signal);
    return {
      generation, deadline,
      refreshAt: deadline - Math.min(300_000, lifetime / 5),
      expiresAt: new Date(requestedWall + lifetime - expiryMargin).toISOString(),
    };
  }

  async retireOwned() {
    const generation = this.ownedGeneration;
    this.ownedGeneration = undefined;
    if (generation === undefined) return;
    try {
      await this.broker.retire(generation, AbortSignal.timeout(1_000));
    } catch (error) {
      // 손실된 reset 응답을 재전송하지 않습니다. 다음 발급은 freshGeneration으로 교체를 입증합니다.
      if (!(error instanceof ProofError) || !["broker_unavailable", "broker_generation_mismatch"].includes(error.code)) {
        this.lastError = "broker_cleanup_failed";
      }
    }
  }

  close() {
    this.closing ??= this.closeOnce();
    return this.closing;
  }

  async closeOnce() {
    this.stop.abort(new DOMException("proof controller closed", "AbortError"));
    this.session = undefined;
    this.state = "STOPPED";
    await this.attempt;
    await this.retireOwned();
    this.broker.close();
  }
}

/** @param {Response} response @param {AbortSignal} signal */
async function readBounded(response, signal) {
  if (!response.body) throw new ProofError("attestation_structure");
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      signal.throwIfAborted();
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > bodyLimit) throw new ProofError("attestation_size");
      chunks.push(value);
    }
    return Buffer.concat(chunks, size).toString("utf8");
  } finally {
    await reader.cancel();
    reader.releaseLock();
  }
}

/** @param {string} raw @returns {unknown} */
function parseJSON(raw) {
  try {
    return JSON.parse(raw);
  } catch {
    throw new ProofError("attestation_structure");
  }
}

/** @param {unknown} value @param {number} limit @returns {value is string} */
function text(value, limit) {
  return typeof value === "string" && value.length > 0 && Buffer.byteLength(value) <= limit;
}

/** @param {unknown} error */
function safeCode(error) {
  return error instanceof ProofError ? error.code : "proof_unavailable";
}
