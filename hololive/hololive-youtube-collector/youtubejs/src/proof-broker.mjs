// @ts-check
import { Agent, request } from "node:http";
import { setTimeout as delay } from "node:timers/promises";

const socketPath = "/run/hololive-youtube-po/worker.sock";
const responseLimit = 64 * 1024;
const requestLimit = 1024 * 1024;
const brokerStates = new Set(["IDLE", "STARTING", "AWAITING_CHALLENGE", "AWAITING_INTEGRITY", "READY"]);
const brokerErrors = new Set([
  "invalid_request", "generation_mismatch", "invalid_state", "expired", "busy", "worker_failed", "worker_timeout",
]);

/** 비신뢰 응답/예외 원문을 진단 경계 밖으로 보내지 않습니다. */
export class ProofError extends Error {
  /** @param {string} code */
  constructor(code) {
    super(code);
    this.name = "ProofError";
    this.code = code;
  }
}

/** 앱 helper socket과 별개인, 네트워크 없는 발급기의 제한된 IPC client입니다. */
export class ProofBrokerClient {
  /** @param {{ socket?: string }} [options] */
  constructor(options = {}) {
    this.socket = options.socket ?? socketPath;
    this.agent = new Agent({ keepAlive: true, maxSockets: 4, maxFreeSockets: 1 });
  }

  /** @param {AbortSignal} signal */
  async freshGeneration(signal) {
    // worker의 30초 SDK 기동과 이전 세대 종료·서비스 재시작을 기다립니다. 발급 요청 예산과는 별개입니다.
    const readySignal = AbortSignal.any([signal, AbortSignal.timeout(40_000)]);
    let retiredGeneration;
    // reset은 한 번만 보냅니다. 응답 유실은 새 generation 관측으로 판정하며 재전송하지 않습니다.
    while (!readySignal.aborted) {
      try {
        const health = await this.exchange("GET", "/health", undefined, readySignal);
        fields(health, ["protocol_version", "generation", "state", "revision"]);
        if (!generation(health.generation) || typeof health.state !== "string" || !brokerStates.has(health.state) || typeof health.revision !== "string") {
          throw new ProofError("broker_protocol");
        }
        if (health.state === "IDLE" && health.generation !== retiredGeneration) {
          return health.generation;
        }
        if (retiredGeneration === undefined) {
          retiredGeneration = health.generation;
          try {
            await this.retire(retiredGeneration, readySignal);
          } catch (error) {
            if (!(error instanceof ProofError) || error.code === "broker_protocol") throw error;
          }
        }
      } catch (error) {
        if (signal.aborted) signal.throwIfAborted();
        if (!(error instanceof ProofError) || !["broker_unavailable", "broker_worker_failed", "broker_busy", "broker_worker_timeout"].includes(error.code)) throw error;
      }
      await delay(50);
    }
    signal.throwIfAborted();
    throw new ProofError("broker_unavailable");
  }

  /** @param {string} id @param {string} userAgent @param {AbortSignal} signal */
  async prepare(id, userAgent, signal) {
    const result = await this.exchange("POST", "/v1/session", {
      protocol_version: 1, generation: id, user_agent: userAgent,
    }, signal);
    fields(result, ["protocol_version", "generation", "prepared"]);
    if (result.generation !== id || result.prepared !== true) throw new ProofError("broker_protocol");
  }

  /**
   * @param {{ generation: string, program: string, global_name: string, interpreter: string }} input
   * @param {AbortSignal} signal
   */
  async challenge(input, signal) {
    const result = await this.exchange("POST", "/v1/challenge", { protocol_version: 1, ...input }, signal);
    fields(result, ["protocol_version", "generation", "snapshot"]);
    if (result.generation !== input.generation || !boundedString(result.snapshot, 60_000)) {
      throw new ProofError("broker_protocol");
    }
    return result.snapshot;
  }

  /** @param {string} id @param {string} integrityToken @param {number} validForMs @param {AbortSignal} signal */
  async activate(id, integrityToken, validForMs, signal) {
    const result = await this.exchange("POST", "/v1/activate", {
      protocol_version: 1, generation: id, integrity_token: integrityToken, valid_for_ms: validForMs,
    }, signal);
    fields(result, ["protocol_version", "generation", "ready"]);
    if (result.generation !== id || result.ready !== true) throw new ProofError("broker_protocol");
  }

  /** @param {string} id @param {string} videoId @param {AbortSignal} signal */
  async mint(id, videoId, signal) {
    const result = await this.exchange("POST", "/v1/mint", {
      protocol_version: 1, generation: id, video_id: videoId,
    }, signal);
    fields(result, ["protocol_version", "generation", "video_id", "po_token"]);
    if (result.generation !== id || result.video_id !== videoId || !boundedString(result.po_token, 8_192) ||
        !/^[A-Za-z0-9_-]+={0,2}$/.test(result.po_token)) {
      throw new ProofError("broker_protocol");
    }
    return result.po_token;
  }

  /** @param {string} id @param {AbortSignal} signal */
  async retire(id, signal) {
    const result = await this.exchange("DELETE", "/v1/session", { protocol_version: 1, generation: id }, signal);
    fields(result, ["protocol_version", "generation", "closed"]);
    if (result.generation !== id || result.closed !== true) throw new ProofError("broker_protocol");
  }

  close() {
    this.agent.destroy();
  }

  /**
   * @param {string} method
   * @param {string} path
   * @param {object | undefined} body
   * @param {AbortSignal} signal
   * @returns {Promise<Record<string, unknown>>}
   */
  exchange(method, path, body, signal) {
    const raw = body === undefined ? undefined : JSON.stringify(body);
    if (raw !== undefined && Buffer.byteLength(raw) > requestLimit) return Promise.reject(new ProofError("broker_request_size"));
    return new Promise((resolve, reject) => {
      const req = request({
        socketPath: this.socket, method, path, agent: this.agent,
        signal: AbortSignal.any([signal, AbortSignal.timeout(8_500)]),
        headers: raw === undefined ? {} : { "content-type": "application/json", "content-length": Buffer.byteLength(raw) },
      }, (res) => {
        let size = 0;
        /** @type {Buffer[]} */
        const chunks = [];
        res.on("data", (chunk) => {
          size += chunk.length;
          if (size > responseLimit) {
            const failure = new ProofError("broker_protocol");
            reject(failure);
            req.destroy(failure);
          } else {
            chunks.push(chunk);
          }
        });
        res.once("error", () => reject(new ProofError("broker_unavailable")));
        res.once("end", () => {
          try {
            if (size > responseLimit) throw new ProofError("broker_protocol");
            if (res.headers["content-type"]?.split(";", 1)[0].trim().toLowerCase() !== "application/json") {
              throw new ProofError("broker_protocol");
            }
            const result = /** @type {unknown} */ (JSON.parse(Buffer.concat(chunks, size).toString("utf8")));
            if (!record(result) || result.protocol_version !== 1) throw new ProofError("broker_protocol");
            if (res.statusCode !== 200) {
              fields(result, ["protocol_version", "error"]);
              if (!record(result.error) || Object.keys(result.error).length !== 1 || typeof result.error.code !== "string" || !brokerErrors.has(result.error.code)) {
                throw new ProofError("broker_protocol");
              }
              throw new ProofError(`broker_${result.error.code}`);
            }
            resolve(result);
          } catch (error) {
            reject(error instanceof ProofError ? error : new ProofError("broker_protocol"));
          }
        });
      });
      req.once("error", (error) => reject(error instanceof ProofError ? error : new ProofError("broker_unavailable")));
      req.end(raw);
    });
  }
}

/** @param {unknown} value @param {string[]} keys */
function fields(value, keys) {
  if (!record(value) || Object.keys(value).length !== keys.length || keys.some((key) => !Object.hasOwn(value, key))) {
    throw new ProofError("broker_protocol");
  }
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} value @param {number} limit @returns {value is string} */
function boundedString(value, limit) {
  return typeof value === "string" && value.length > 0 && Buffer.byteLength(value) <= limit;
}

/** @param {unknown} value @returns {value is string} */
function generation(value) {
  return boundedString(value, 128) && /^[A-Za-z0-9_-]+$/.test(value);
}
