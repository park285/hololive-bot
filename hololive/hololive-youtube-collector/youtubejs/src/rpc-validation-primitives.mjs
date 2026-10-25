// @ts-check

import { RpcRequestError, RpcResponseError } from "./rpc-errors.mjs";

export const maxChannelIdentifierBytes = 256;
export const maxVideoIdentifierBytes = 128;
// live 채널 요청이 지정할 수 있는 예정 영상 player 조회 수의 protocol 상한입니다. 실제 상한은 collector가 요청마다 정합니다.
export const maxScheduleMetadataLookups = 32;

/** @param {unknown} value @returns {Record<string, unknown>} */
export function requestRecord(value) {
  if (!isRecord(value)) {
    throw new RpcRequestError("request must be a JSON object");
  }
  return value;
}

/** @typedef {{ readonly required: readonly string[], readonly allowed: ReadonlySet<string> }} KeySchema */

/**
 * 요청마다 배열·Set을 만들지 않도록 모듈 로드 시 한 번 만드는 불변 key 목록입니다.
 * @param {readonly string[]} required
 * @param {readonly string[]} [optional]
 * @returns {KeySchema}
 */
export function keySchema(required, optional = []) {
  return Object.freeze({ required: Object.freeze([...required]), allowed: new Set([...required, ...optional]) });
}

/** @param {Record<string, unknown>} record @param {KeySchema} schema */
export function assertRequestKeys(record, schema) {
  assertExactKeys(record, schema, RpcRequestError);
}

/** @param {Record<string, unknown>} record @param {KeySchema} schema */
export function assertResponseKeys(record, schema) {
  assertExactKeys(record, schema, RpcResponseError);
}

/**
 * unknown key를 record 순서대로 먼저 거부한 뒤 required key 누락을 선언 순서대로 거부합니다.
 * @param {Record<string, unknown>} record
 * @param {KeySchema} schema
 * @param {typeof RpcRequestError | typeof RpcResponseError} ErrorType
 */
function assertExactKeys(record, schema, ErrorType) {
  for (const key in record) {
    if (Object.hasOwn(record, key) && !schema.allowed.has(key)) {
      throw new ErrorType(`unknown field: ${key}`);
    }
  }
  for (const key of schema.required) {
    if (!Object.hasOwn(record, key)) {
      throw new ErrorType(`${key} is required`);
    }
  }
}

/** @param {Record<string, unknown>} record */
export function protocolVersion(record) {
  if (record.protocol_version !== 1) {
    throw new RpcRequestError("protocol_version must be 1");
  }
  return 1;
}

/** @param {Record<string, unknown>} record */
export function responseProtocolVersion(record) {
  if (record.protocol_version !== 1) {
    throw new RpcResponseError("protocol_version must be 1");
  }
  return 1;
}

/** @param {Record<string, unknown>} record @param {string} field */
export function positiveInteger(record, field) {
  const value = record[field];
  if (!Number.isSafeInteger(value) || Number(value) <= 0) {
    throw new RpcRequestError(`${field} must be a positive integer`);
  }
  return Number(value);
}

/** @param {unknown} value @returns {Record<string, unknown>} */
export function responseRecord(value) {
  if (!isRecord(value)) {
    throw new RpcResponseError("response must be a JSON object");
  }
  return value;
}

/** @param {unknown} value @returns {value is Record<string, unknown>} */
export function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** @param {Record<string, unknown>} record @param {string} field */
export function requiredString(record, field) {
  const value = record[field];
  if (typeof value !== "string" || value.trim() === "") {
    throw new RpcRequestError(`${field} is required`);
  }
  return value.trim();
}

/** @param {Record<string, unknown>} record @param {string} field */
export function arrayField(record, field) {
  const value = record[field];
  if (!Array.isArray(value)) throw new RpcResponseError(`${field} must be an array`);
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
export function recordField(record, field) {
  const value = record[field];
  if (!isRecord(value)) throw new RpcResponseError(`${field} must be an object`);
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
export function nonemptyStringField(record, field) {
  const value = record[field];
  if (typeof value !== "string" || value.trim() === "") {
    throw new RpcResponseError(`${field} must be a non-empty string`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
export function stringField(record, field) {
  const value = record[field];
  if (typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field */
export function optionalResponseString(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
export function optionalNullableString(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (value !== null && typeof value !== "string") {
    throw new RpcResponseError(`${field} must be a string or null`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
export function optionalBoolean(record, field) {
  const value = record[field];
  if (value === undefined) return {};
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field */
export function nonnegativeIntegerField(record, field) {
  const value = record[field];
  if (!Number.isSafeInteger(value) || Number(value) < 0) {
    throw new RpcResponseError(`${field} must be a non-negative integer`);
  }
  return Number(value);
}


/** @param {Record<string, unknown>} record @param {string} field */
export function optionalRFC3339(record, field) {
  const value = optionalResponseTimestamp(record, field);
  return value === undefined ? {} : { [field]: value };
}

/** @param {Record<string, unknown>} record @param {string} field @returns {string | undefined} */
export function optionalResponseTimestamp(record, field) {
  const value = record[field];
  if (value === undefined) return undefined;
  const parsed = typeof value === "string" ? Date.parse(value) : Number.NaN;
  if (typeof value !== "string" || !Number.isFinite(parsed) || new Date(parsed).toISOString() !== value) {
    throw new RpcResponseError(`${field} must be an RFC3339 timestamp`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @returns {boolean | undefined} */
export function optionalResponseBoolean(record, field) {
  const value = record[field];
  if (value === undefined) return undefined;
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @returns {boolean} */
export function requiredResponseBoolean(record, field) {
  const value = record[field];
  if (typeof value !== "boolean") {
    throw new RpcResponseError(`${field} must be boolean`);
  }
  return value;
}

// Go 관측 계약의 식별자 규칙과 같게 빈 값·앞뒤 공백·길이 초과를 거부합니다. 요청 subject를 변형하지 않고 그대로 되돌려야 합니다.
/** @param {unknown} value @param {number} maxBytes */
function validIdentifier(value, maxBytes) {
  return typeof value === "string" && value.trim() !== "" && value.trim() === value && Buffer.byteLength(value) <= maxBytes;
}

/** @param {Record<string, unknown>} record @param {string} field @param {number} maxBytes @returns {string} */
export function requestIdentifier(record, field, maxBytes) {
  const value = record[field];
  if (typeof value !== "string" || !validIdentifier(value, maxBytes)) {
    throw new RpcRequestError(`${field} must be a trimmed identifier of at most ${maxBytes} bytes`);
  }
  return value;
}

/** @param {Record<string, unknown>} record @param {string} field @param {number} maxBytes @returns {string} */
export function responseIdentifier(record, field, maxBytes) {
  const value = record[field];
  if (typeof value !== "string" || !validIdentifier(value, maxBytes)) {
    throw new RpcResponseError(`${field} must be a trimmed identifier of at most ${maxBytes} bytes`);
  }
  return value;
}
