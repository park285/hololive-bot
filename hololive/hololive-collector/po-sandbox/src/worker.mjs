import { createInterface } from 'node:readline';
import { JSDOM } from 'jsdom';
import { BotGuardClient } from 'bgutils-js/botguard';
import { WebPoMinter } from 'bgutils-js/webpo';

for (const method of ['log', 'info', 'warn', 'error', 'debug', 'trace']) {
  console[method] = () => {};
}

const output = process.stdout.write.bind(process.stdout);
const reply = (value) => output(`${JSON.stringify(value)}\n`);

let dom;
let bot;
let minter;
const signals = [];
const input = createInterface({ input: process.stdin, crlfDelay: Infinity });

reply({ type: 'loaded' });

try {
  for await (const line of input) {
    const message = JSON.parse(line);
    switch (message.type) {
      case 'prepare':
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
        break;
      case 'challenge':
        new Function(message.interpreter)();
        bot = await BotGuardClient.create({
          program: message.program, globalName: message.global_name, globalObject: globalThis,
        });
        reply({ type: 'snapshot', snapshot: await bot.snapshot({ webPoSignalOutput: signals }, 8000) });
        break;
      case 'activate':
        minter = await WebPoMinter.create({ integrityToken: message.integrity_token }, signals);
        reply({ type: 'ready', ready: true });
        break;
      case 'mint':
        reply({ type: 'minted', video_id: message.video_id, po_token: await minter.mintAsWebsafeString(message.video_id) });
        break;
      default:
        throw new Error('invalid operation');
    }
  }
} catch {
  reply({ type: 'error' });
  process.exitCode = 1;
} finally {
  input.close();
  if (bot) await bot.shutdown().catch(() => {});
  dom?.window.close();
}
