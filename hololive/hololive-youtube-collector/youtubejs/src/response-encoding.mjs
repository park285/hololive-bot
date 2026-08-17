const encodedBodies = new WeakMap();

export function encodeResponseBody(body) {
  if (body !== null && typeof body === "object") {
    const cached = encodedBodies.get(body);
    if (cached !== undefined) {
      return cached;
    }
    const raw = JSON.stringify(body);
    encodedBodies.set(body, raw);
    return raw;
  }
  return JSON.stringify(body);
}
