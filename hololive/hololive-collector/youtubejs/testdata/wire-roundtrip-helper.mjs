import { readFileSync } from "node:fs";
import { recordScheduleLookup } from "../src/request-context.mjs";
import { listenUnix } from "../src/server.mjs";

const fixture = JSON.parse(readFileSync(new URL("./wire-contract.json", import.meta.url), "utf8"));
const minimalSubject = "UCminimal";

/**
 * @param {string} path
 * @param {string} subject
 */
function respond(path, subject) {
  const failure = fixture.roundtrip_failures.find((item) => item.subject === subject);
  if (failure != null) {
    for (let index = 0; index < (failure.schedule_lookups ?? 0); index += 1) recordScheduleLookup();
    throw Object.assign(new Error(`fixture ${failure.code}`), {
      code: failure.code,
      ...(failure.retry == null ? {} : { retry: failure.retry }),
    });
  }
  const cases = fixture.endpoints[path];
  const body = subject === minimalSubject
    ? cases.minimal[0]
    : cases.valid.find((item) => item.subject === subject)?.body;
  if (body == null) {
    throw Object.assign(new Error("fixture subject is unknown"), { code: "helper_internal_invariant" });
  }
  const { protocol_version: _protocolVersion, ...result } = structuredClone(body);
  return result;
}

/** @param {string[]} argv */
function parseArgs(argv) {
  const args = { socket: "", requestReadTimeoutMs: 30_000 };
  for (let index = 0; index < argv.length; index += 1) {
    if (argv[index] === "--socket") args.socket = String(argv[index + 1] ?? "");
    if (argv[index] === "--request-read-timeout-ms") args.requestReadTimeoutMs = Number(argv[index + 1] ?? 30_000);
  }
  return args;
}

const args = parseArgs(process.argv.slice(2));
await listenUnix(args.socket, {
  requestReadTimeoutMs: args.requestReadTimeoutMs,
  manageProcess: true,
  fetchers: {
    fetchCommunity: ({ channelId }) => respond("/v1/community", channelId),
    fetchContent: ({ channelId }) => respond("/v1/content", channelId),
    fetchChannel: ({ channelId }) => respond("/v1/channel", channelId),
    fetchChannelLiveCheck: ({ channelId }) => respond("/v1/channel_live_check", channelId),
    fetchVideoLiveCheck: ({ videoId }) => respond("/v1/video_live_check", videoId),
  },
});
