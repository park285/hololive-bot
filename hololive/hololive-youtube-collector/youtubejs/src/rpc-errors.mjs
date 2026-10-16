// @ts-check

export class RpcRequestError extends Error {}

export class RpcResponseError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "RpcResponseError";
    this.code = "parser_drift";
    this.failureClass = "DATA_CONTRACT";
  }
}

export class RpcProtocolError extends Error {
  constructor(message = "") {
    super(message);
    this.name = "RpcProtocolError";
    this.code = "helper_protocol_mismatch";
    this.failureClass = "PROTOCOL";
  }
}
