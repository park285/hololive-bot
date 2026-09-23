import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { allowedLoginURL, loginWithPage } from './src/workflow.mjs';

// 네트워크가 없는 테스트 컨테이너에서만 X 화면을 가짜 HTML로 제공합니다.
assert.equal(process.getuid(), 1000);
const browser = await chromium.launch({ headless: true, chromiumSandbox: true, timeout: 20_000 });
try {
  for (const mode of ['success', 'challenge']) {
    const context = await browser.newContext();
    await context.route('**/*', async (route) => {
      const url = new URL(route.request().url());
      assert.equal(allowedLoginURL(url.href), true);
      if (url.origin === 'https://api.x.com' && url.pathname === '/1.1/onboarding/task.json') {
        if (mode === 'success') await context.addCookies([
          { name: 'auth_token', value: 'a'.repeat(40), url: 'https://x.com', httpOnly: true, secure: true },
          { name: 'ct0', value: 'b'.repeat(64), url: 'https://x.com', secure: true },
        ]);
        await route.fulfill({ status: 200, headers: { 'access-control-allow-origin': 'https://x.com' }, body: 'ok' });
      } else {
        await route.fulfill({ contentType: 'text/html', body: `
          <form id="loginForm">
          <input id="username" autocomplete="username webauthn">
          <input name="password" type="password" inert>
          <input id="password" name="password" type="password" hidden>
          <button type="submit">Continue</button><div id="result"></div>
          </form><input autocomplete="username webauthn">
          <script>
          loginForm.onsubmit = async (event) => {
            event.preventDefault();
            if (password.hidden) { password.hidden=false; return; }
            await fetch('https://api.x.com/1.1/onboarding/task.json');
            ${mode === 'challenge' ? 'result.innerHTML=\'<input autocomplete="one-time-code">\';' : ''}
          };
          </script>` });
      }
    });
    const result = await loginWithPage(await context.newPage(), { username: 'example', password: 'fixture-only-password' });
    if (mode === 'success') assert.equal(result.cookies?.auth_token, 'a'.repeat(40));
    else assert.deepEqual(result, { error: 'additional_authentication' });
    await context.close();
  }
  console.log('sandboxed Linux login fixtures passed: success, additional authentication');
} finally {
  await browser.close();
}
