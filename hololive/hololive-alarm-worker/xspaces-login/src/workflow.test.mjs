import assert from 'node:assert/strict';
import test from 'node:test';
import { allowedLoginURL, loginWithPage, validateCredentials } from './workflow.mjs';

const credentials = { username: 'example', password: 'fixture-password' };

test('로그인 API는 허용하고 외부 호스트·포트·인증 URL은 차단한다', () => {
  for (const url of ['https://x.com/i/flow/login', 'https://api.x.com/1.1/onboarding/task.json', 'https://abs.twimg.com/script.js', 'https://pbs.twimg.com/image.png']) {
    assert.equal(allowedLoginURL(url), true);
  }
  for (const url of ['http://x.com', 'https://x.com:8443', 'https://x.com.example.org', 'https://example.org', 'https://other.x.com', 'https://name:password@x.com', 'file:///etc/passwd']) {
    assert.equal(allowedLoginURL(url), false);
  }
});

function fixture(mode) {
  const actions = [];
  const page = {
    setDefaultTimeout() {},
    async goto(url) { actions.push(['goto', url]); if (mode === 'network') throw new Error('secret'); },
    url: () => mode === 'redirect' ? 'https://example.org' : 'https://x.com/i/jf/onboarding/web',
    locator(selector) {
      return {
        first() { return this; },
        async fill(value) { actions.push(['fill', selector, value]); },
        async press() {
          if (!selector.includes('name="password"')) return;
          if (mode === 'interrupted') throw new Error('secret');
          actions.push(['submit']);
        },
        async waitFor() { if (['challenge-before', 'changed-page'].includes(mode)) throw new Error('secret'); },
        async count() { return ['challenge-before', 'challenge-after'].includes(mode) ? 1 : 0; },
      };
    },
    getByRole(role) {
      return {
        async allTextContents() { return mode === 'rejected' ? ['Wrong password!'] : []; },
      };
    },
    context() {
      return { async cookies() { return mode === 'success' ? [
        { name: 'auth_token', value: 'a'.repeat(40) }, { name: 'ct0', value: 'b'.repeat(64) },
      ] : []; } };
    },
    async waitForTimeout() {},
  };
  return { page, actions };
}

test('일반 로그인은 두 쿠키만 반환한다', async () => {
  const { page, actions } = fixture('success');
  const result = await loginWithPage(page, credentials);
  assert.deepEqual(result, { cookies: { auth_token: 'a'.repeat(40), ct0: 'b'.repeat(64) } });
  assert.equal(actions.filter((action) => action[0] === 'submit').length, 1);
});

for (const [mode, error] of [
  ['challenge-before', 'additional_authentication'], ['challenge-after', 'additional_authentication'],
  ['interrupted', 'outcome_unknown'], ['timeout', 'outcome_unknown'],
  ['rejected', 'login_rejected'], ['network', 'browser_failed'], ['redirect', 'browser_failed'], ['changed-page', 'browser_failed'],
]) {
  test(`${mode}: 고정 결과만 반환하고 재시도하지 않는다`, async () => {
    const { page, actions } = fixture(mode);
    assert.deepEqual(await loginWithPage(page, credentials), { error });
    assert.ok(actions.filter((action) => action[0] === 'submit').length <= 1);
    if (mode === 'redirect') assert.equal(actions.filter((action) => action[0] === 'fill').length, 0);
  });
}

test('입력은 아이디와 비밀번호만 허용한다', () => {
  assert.deepEqual(validateCredentials(credentials), credentials);
  for (const value of [null, [], { ...credentials, token: 'secret' }, { ...credentials, username: '../other' }, { ...credentials, password: '' }]) {
    assert.throws(() => validateCredentials(value), { message: 'invalid_input' });
  }
});
