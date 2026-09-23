const cookiePattern = /^[a-zA-Z0-9_-]{16,512}$/;
const challengeSelector = 'input[autocomplete="one-time-code"]:visible, iframe[src*="captcha"]:visible, iframe[src*="arkoselabs"]:visible, input[data-testid="ocfEnterTextTextInput"]:not([autocomplete~="username"]):visible';

/** X 로그인 API와 화면 자산의 HTTPS 요청만 허용합니다. 하위 도메인 전체를 신뢰하지 않습니다. */
export function allowedLoginURL(value) {
  const url = new URL(value);
  return url.protocol === 'https:' && url.port === '' && url.username === '' && url.password === ''
    && ['x.com', 'api.x.com', 'abs.twimg.com', 'pbs.twimg.com'].includes(url.hostname);
}

/** 보호된 설정에서 전달한 두 로그인 값만 허용합니다. 추가 인증 비밀은 받지 않습니다. */
export function validateCredentials(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)
    || Object.keys(value).some((key) => !['username', 'password'].includes(key))
    || typeof value.username !== 'string' || !/^[a-zA-Z0-9_]{1,15}$/.test(value.username)
    || typeof value.password !== 'string' || value.password.length < 1 || value.password.length > 1024
    || /[\u0000-\u001f\u007f]/u.test(value.password)) throw new Error('invalid_input');
  return { username: value.username, password: value.password };
}

/** X의 일반 로그인 화면만 처리합니다. CAPTCHA·추가 인증은 우회하지 않습니다. */
export async function loginWithPage(page, credentials) {
  let submitted = false;
  try {
    page.setDefaultTimeout(15_000);
    await page.goto('https://x.com/i/jf/onboarding/web?lang=en', { waitUntil: 'domcontentloaded', timeout: 25_000 });
    if (new URL(page.url()).origin !== 'https://x.com') return { error: 'browser_failed' };
    // 현재 X 화면은 모달과 배경에 같은 폼을 렌더링하고 autocomplete 토큰에 webauthn을 추가합니다.
    const username = page.locator('input[autocomplete~="username"]:not([inert]):visible').first();
    await username.fill(credentials.username);
    await username.press('Enter');
    // Jetfuel의 자동완성용 inert 비밀번호 입력은 실제 비밀번호 단계가 아닙니다.
    const password = page.locator('input[name="password"]:not([inert]):visible').first();
    try { await password.waitFor({ state: 'visible', timeout: 15_000 }); }
    catch { return { error: await page.locator(challengeSelector).count() > 0 ? 'additional_authentication' : 'browser_failed' }; }
    if (new URL(page.url()).origin !== 'https://x.com') return { error: 'browser_failed' };
    await password.fill(credentials.password);
    // 이 시점 이후 timeout/화면 변경은 로그인 부작용이 있었는지 알 수 없습니다.
    submitted = true;
    await password.press('Enter');
    for (let step = 0; step < 30; step++) {
      const cookies = await page.context().cookies('https://x.com');
      const auth = cookies.find((cookie) => cookie.name === 'auth_token');
      const csrf = cookies.find((cookie) => cookie.name === 'ct0');
      if (auth && csrf && cookiePattern.test(auth.value) && cookiePattern.test(csrf.value)) {
        return { cookies: { auth_token: auth.value, ct0: csrf.value } };
      }
      if (await page.locator(challengeSelector).count() > 0) {
        return { error: 'additional_authentication' };
      }
      const alerts = await page.getByRole('alert').allTextContents();
      if (alerts.some((text) => /(?:wrong|incorrect) password|could not find your account/i.test(text))) return { error: 'login_rejected' };
      await page.waitForTimeout(1_000);
    }
    return { error: 'outcome_unknown' };
  } catch {
    return { error: submitted ? 'outcome_unknown' : 'browser_failed' };
  }
}
