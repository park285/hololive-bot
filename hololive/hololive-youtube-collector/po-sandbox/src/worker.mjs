import { createInterface } from 'node:readline';
import { JSDOM } from 'jsdom';
import { BotGuardClient } from 'bgutils-js/botguard';
import { WebPoMinter } from 'bgutils-js/webpo';

// This process has no network namespace, credentials, or writable application tree.
// In particular, interpreter code never runs in the HTTP broker's address space.
for (const method of ['log', 'info', 'warn', 'error', 'debug', 'trace']) {
  console[method] = () => {};
}

const output = process.stdout.write.bind(process.stdout);
const reply = (value) => output(`${JSON.stringify(value)}\n`);
const fields = (value, names) => value !== null && typeof value === 'object' && !Array.isArray(value) &&
  Object.keys(value).length === names.length && names.every((name) => Object.hasOwn(value, name));
const bounded = (value, max) => typeof value === 'string' && value.length > 0 && Buffer.byteLength(value, 'utf8') <= max;

let phase = 'IDLE';
let dom;
let bot;
let minter;
const signals = [];
const input = createInterface({ input: process.stdin, crlfDelay: Infinity });

// 신뢰된 SDK import 완료를 먼저 알린다. UA/JSDOM 준비와 외부 interpreter는
// 이후 generation에 묶인 별도 명령에서만 실행한다.
reply({ type: 'loaded' });

try {
  for await (const line of input) {
    if (Buffer.byteLength(line, 'utf8') > 1024 * 1024) throw new Error('frame too large');
    const message = JSON.parse(line);
    if (phase === 'IDLE' && fields(message, ['type', 'user_agent']) && message.type === 'prepare' &&
        bounded(message.user_agent, 1024)) {
      dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', {
        url: 'https://www.youtube.com/', referrer: 'https://www.youtube.com/',
        resources: { userAgent: message.user_agent },
      });
      Object.assign(globalThis, {
        window: dom.window, document: dom.window.document,
        location: dom.window.location, origin: dom.window.origin,
      });
      if (!Reflect.has(globalThis, 'navigator')) {
        Object.defineProperty(globalThis, 'navigator', { value: dom.window.navigator });
      }
      if (navigator.userAgent !== message.user_agent) throw new Error('user agent mismatch');
      reply({ type: 'prepared', prepared: true });
      phase = 'AWAITING_CHALLENGE';
    } else if (phase === 'AWAITING_CHALLENGE' && fields(message, ['type', 'program', 'global_name', 'interpreter']) &&
        message.type === 'challenge' && bounded(message.program, 1024 * 1024) &&
        bounded(message.global_name, 1024) && bounded(message.interpreter, 1024 * 1024)) {
      new Function(message.interpreter)();
      bot = await BotGuardClient.create({
        program: message.program, globalName: message.global_name, globalObject: globalThis,
      });
      const snapshot = await bot.snapshot({ webPoSignalOutput: signals }, 8000);
      if (!bounded(snapshot, 60000)) throw new Error('invalid snapshot');
      reply({ type: 'snapshot', snapshot });
      phase = 'AWAITING_INTEGRITY';
    } else if (phase === 'AWAITING_INTEGRITY' && fields(message, ['type', 'integrity_token']) &&
        message.type === 'activate' && bounded(message.integrity_token, 1024 * 1024)) {
      minter = await WebPoMinter.create({ integrityToken: message.integrity_token }, signals);
      reply({ type: 'ready', ready: true });
      phase = 'READY';
    } else if (phase === 'READY' && fields(message, ['type', 'video_id']) &&
        message.type === 'mint' && bounded(message.video_id, 128)) {
      const poToken = await minter.mintAsWebsafeString(message.video_id);
      if (!bounded(poToken, 8192)) throw new Error('invalid minted token');
      reply({ type: 'minted', video_id: message.video_id, po_token: poToken });
    } else {
      throw new Error('invalid operation');
    }
  }
} catch {
  // The broker treats any worker failure as fatal and retires the whole service.
  reply({ type: 'error' });
  process.exitCode = 1;
} finally {
  input.close();
  if (bot) await bot.shutdown().catch(() => {});
  dom?.window.close();
}
