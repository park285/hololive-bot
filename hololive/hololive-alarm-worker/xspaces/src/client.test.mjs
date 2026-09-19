import assert from 'node:assert/strict';
import test from 'node:test';
import { boundedFetch, collectSpaces, validateCookies } from './client.mjs';

const cookies = { auth_token: 'a'.repeat(40), ct0: 'b'.repeat(64) };
const transaction = { generateTransactionId: async () => 'test-transaction' };
const id = '1abcXYZ';
const json = (body, status = 200, headers = {}) => new Response(JSON.stringify(body), { status, headers });
const fleet = { users: { '123': { spaces: { live_content: { audiospace: { broadcast_id: id } } } } } };
const metadata = { rest_id: id, state: 'Running', creator_results: { result: { rest_id: '123' } }, title: '스페이스\n시작', started_at: Date.UTC(2026, 8, 19) };

test('계정으로 스페이스를 발견하고 내부 GET 경로에만 쿠키를 보낸다', async () => {
  const calls = [];
  const fetch = boundedFetch(async (url, init) => {
    calls.push(url);
    assert.equal(init.method, 'GET');
    assert.equal(init.redirect, 'error');
    assert.equal(new Headers(init.headers).get('cookie'), `auth_token=${cookies.auth_token}; ct0=${cookies.ct0}`);
    return calls.length === 1 ? json(fleet) : json({ data: { audioSpace: { metadata } } });
  });
  const result = await collectSpaces(['123'], cookies, transaction, fetch);
  assert.deepEqual(result, [{ space_id: id, creator_id: '123', title: '스페이스 시작', started_at: '2026-09-19T00:00:00.000Z' }]);
  assert.equal(calls[0].searchParams.get('user_ids'), '123');
  assert.equal(calls[1].origin, 'https://x.com');
});

test('예약·종료와 다른 개설자의 스페이스는 시작 알림에서 제외한다', async () => {
  for (const change of [{ state: 'Ended' }, { state: 'NotStarted' }, { creator_results: { result: { rest_id: '999' } } }]) {
    let calls = 0;
    const result = await collectSpaces(['123'], cookies, transaction, async () => ++calls === 1 ? json(fleet) : json({ data: { audioSpace: { metadata: { ...metadata, ...change } } } }));
    assert.deepEqual(result, []);
  }
});

test('빈 조회와 깨진 응답·인증 실패·부분 오류를 구분한다', async () => {
  assert.deepEqual(await collectSpaces(['123'], cookies, transaction, async () => json({ users: {} })), []);
  for (const [response, code] of [[{}, 'invalid_response'], [{ users: null }, 'invalid_response'], [{ users: {}, errors: [{ code: 1 }] }, 'api_error'], [{ users: { unexpected: {} } }, 'unexpected_user']]) {
    await assert.rejects(collectSpaces(['123'], cookies, transaction, async () => json(response)), { code });
  }
  for (const status of [401, 403]) {
    await assert.rejects(collectSpaces(['123'], cookies, transaction, async () => json({ secret: cookies.auth_token }, status)), { code: 'authentication' });
  }
  await assert.rejects(collectSpaces(['123'], cookies, transaction, async () => json({}, 429, { 'retry-after': '600' })), (error) => error.code === 'rate_limited' && error.cooldownSeconds === 600);
});

test('잘못된 상태·시각·식별자와 중복 대상은 성공으로 바뀌지 않는다', async () => {
  for (const change of [{ state: 'Unknown' }, { started_at: '2026-09-19' }, { rest_id: 'wrong' }]) {
    let calls = 0;
    await assert.rejects(collectSpaces(['123'], cookies, transaction, async () => ++calls === 1 ? json(fleet) : json({ data: { audioSpace: { metadata: { ...metadata, ...change } } } })), { code: 'invalid_response' });
  }
  await assert.rejects(collectSpaces(['123', '123'], cookies, transaction, () => assert.fail('외부 요청 금지')), { code: 'invalid_targets' });
  assert.throws(() => validateCookies({ ...cookies, ct0: 'bad\r\nvalue' }), { code: 'invalid_cookies' });
});

test('공식 API·유료 공급자·쓰기·credential 유출 경로를 네트워크 전송 전에 거부한다', async () => {
  const fetch = boundedFetch(() => assert.fail('금지된 네트워크 요청'));
  for (const url of ['https://api.x.com/2/spaces', 'https://paid.example/spaces', 'http://x.com/home', 'https://x.com/i/api/graphql/other/TweetCreate']) {
    await assert.rejects(fetch(url), { code: 'forbidden_endpoint' });
  }
  await assert.rejects(fetch('https://x.com/home', { method: 'POST' }), { code: 'forbidden_endpoint' });
  await assert.rejects(fetch('https://abs.twimg.com/responsive-web/client-web/main.js', { headers: { cookie: 'secret' } }), { code: 'forbidden_credentials' });
});

test('redirect와 과도한 응답을 거부한다', async () => {
  await assert.rejects(boundedFetch(async () => new Response('redirect', { status: 302 }))('https://x.com/home'), { code: 'redirect' });
  await assert.rejects(boundedFetch(async () => new Response('x'.repeat(2 * 1024 * 1024 + 1)))('https://x.com/home'), { code: 'response_too_large' });
});
