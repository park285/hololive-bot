import { chromium } from 'playwright';
import { allowedLoginURL, loginWithPage, validateCredentials } from './workflow.mjs';

// 인증 값과 브라우저 오류 원문은 stdout/stderr에 출력하지 않습니다.
// stdout은 부모 프로세스가 직접 읽는 비밀 전달 파이프이며 로그와 연결하면 안 됩니다.
async function main() {
  const chunks = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += chunk.length;
    if (size > 8192) throw new Error('invalid_input');
    chunks.push(chunk);
  }
  const raw = Buffer.concat(chunks);
  let input;
  try { input = JSON.parse(raw.toString('utf8')); } finally { raw.fill(0); for (const chunk of chunks) chunk.fill(0); }
  const credentials = validateCredentials(input);
  const browser = await chromium.launch({ headless: true, chromiumSandbox: true, timeout: 20_000 });
  try {
    const context = await browser.newContext({ locale: 'en-US', serviceWorkers: 'block', acceptDownloads: false });
    await context.route('**/*', async (route) => {
      if (allowedLoginURL(route.request().url())) await route.continue(); else await route.abort('blockedbyclient');
    });
    const page = await context.newPage();
    return await loginWithPage(page, credentials);
  } finally {
    credentials.password = '';
    await browser.close();
  }
}

// 인자·환경·trace·녹화에 비밀을 넣지 않으며 파싱/실행 오류도 고정 어휘만 반환합니다.
main().then(
  (result) => process.stdout.write(JSON.stringify(result)),
  () => { process.stdout.write(JSON.stringify({ error: 'outcome_unknown' })); process.exitCode = 1; },
);
