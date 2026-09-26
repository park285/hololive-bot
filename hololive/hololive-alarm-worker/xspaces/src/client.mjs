// X 웹 내부 API만 사용한다. 공식 API 키, 공급자 URL, 유료 대체 경로는 받지 않는다.
const fleetPath = '/i/api/fleets/v1/avatar_content';
const spacePath = '/i/api/graphql/kZ9wfR8EBtiP0As3sFFrBA/AudioSpaceById';
// X 웹 앱에 공개된 application bearer다. 계정 인증 값은 부모 프로세스의 stdin으로만 받는다.
const webBearer = 'Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA';
const userAgent = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36';
const features = {
  spaces_2022_h2_clipping: true, spaces_2022_h2_spaces_communities: true,
  responsive_web_graphql_exclude_directive_enabled: true, verified_phone_label_enabled: false,
  creator_subscriptions_tweet_preview_api_enabled: true,
  responsive_web_graphql_skip_user_profile_image_extensions_enabled: false,
  tweetypie_unmention_optimization_enabled: true, responsive_web_edit_tweet_api_enabled: true,
  graphql_is_translatable_rweb_tweet_is_translatable_enabled: true, view_counts_everywhere_api_enabled: true,
  longform_notetweets_consumption_enabled: true, responsive_web_twitter_article_tweet_consumption_enabled: false,
  tweet_awards_web_tipping_enabled: false, freedom_of_speech_not_reach_fetch_enabled: true,
  standardized_nudges_misinfo: true, tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled: true,
  responsive_web_graphql_timeline_navigation_enabled: true, longform_notetweets_rich_text_read_enabled: true,
  longform_notetweets_inline_media_enabled: true, responsive_web_media_download_video_enabled: false,
  responsive_web_enhance_cards_enabled: false,
};

/** 원문·쿠키 없이 고정 오류 코드와 제한된 HTTP 진단 정보만 전달한다. */
export class CollectionError extends Error {
  constructor(code, cooldownSeconds = 0, httpStatus = 0, apiCodes = []) {
    super(code);
    this.code = code;
    this.cooldownSeconds = cooldownSeconds;
    this.httpStatus = httpStatus;
    this.apiCodes = apiCodes;
  }
}

/** 요청 ID 라이브러리의 통신까지 공개 웹 자료와 두 읽기 경로로 한정한다. */
export function boundedFetch(fetchImpl) {
  return async (input, init = {}) => {
    const url = new URL(input instanceof Request ? input.url : input);
    const method = (init.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const api = url.origin === 'https://x.com' && [fleetPath, spacePath].includes(url.pathname);
    const asset = url.origin === 'https://abs.twimg.com' && /^\/responsive-web\/client-web\/[^/]+\.js$/.test(url.pathname);
    // 로그아웃 상태의 /home은 로그인 화면으로 redirect되므로 요청 ID 라이브러리는 /i/jf/ 앱 셸만 읽는다.
    const appShell = url.href === 'https://x.com/i/jf/';
    if (method !== 'GET' || url.username || url.password || (!api && !asset && !appShell)) {
      throw new CollectionError('forbidden_endpoint');
    }
    const headers = new Headers(init.headers ?? (input instanceof Request ? input.headers : undefined));
    if (!api && (headers.has('cookie') || headers.has('authorization'))) {
      throw new CollectionError('forbidden_credentials');
    }
    const response = await fetchImpl(url, {
      ...init, method: 'GET', headers, redirect: 'error',
      signal: AbortSignal.any([AbortSignal.timeout(10000), ...(init.signal ? [init.signal] : [])]),
    });
    if (response.status >= 300 && response.status < 400) throw new CollectionError('redirect');
    const reader = response.body?.getReader();
    if (!reader) throw new CollectionError('invalid_response');
    const chunks = [];
    let size = 0;
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > 2 * 1024 * 1024) throw new CollectionError('response_too_large');
        chunks.push(value);
      }
    } finally {
      await reader.cancel();
    }
    return new Response(Buffer.concat(chunks), { status: response.status, headers: response.headers });
  };
}

function object(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new CollectionError('invalid_response');
  return value;
}

/** cookie 헤더에 삽입할 두 값만 허용하며 다른 브라우저 정보를 받지 않는다. */
export function validateCookies(value) {
  object(value);
  for (const key of ['auth_token', 'ct0']) {
    if (typeof value[key] !== 'string' || !/^[a-zA-Z0-9_-]{16,512}$/.test(value[key])) {
      throw new CollectionError('invalid_cookies');
    }
  }
  if (Object.keys(value).some((key) => !['auth_token', 'ct0'].includes(key))) throw new CollectionError('invalid_cookies');
  return { auth_token: value.auth_token, ct0: value.ct0 };
}

/** 사용자 ID를 한 번에 최대 100개 조회하고 직접 개설한 Running 스페이스만 반환한다. */
export async function collectSpaces(userIDs, cookies, transaction, fetchImpl) {
  if (!Array.isArray(userIDs) || userIDs.length < 1 || userIDs.length > 100 ||
      userIDs.some((id) => typeof id !== 'string' || !/^[0-9]{1,20}$/.test(id)) ||
      new Set(userIDs).size !== userIDs.length) throw new CollectionError('invalid_targets');
  validateCookies(cookies);
  async function request(path, params) {
    const url = new URL(path, 'https://x.com');
    url.search = new URLSearchParams(params).toString();
    const transactionID = await transaction.generateTransactionId('GET', path);
    const res = await fetchImpl(url, { headers: {
      authorization: webBearer, cookie: `auth_token=${cookies.auth_token}; ct0=${cookies.ct0}`,
      'x-csrf-token': cookies.ct0, 'x-client-transaction-id': transactionID,
      'x-twitter-active-user': 'yes', 'x-twitter-auth-type': 'OAuth2Session',
      'x-twitter-client-language': 'en', 'user-agent': userAgent, accept: 'application/json',
    } });
    if (res.status === 429) {
      const reset = Number(res.headers.get('x-rate-limit-reset')) - Date.now() / 1000;
      const delay = Number(res.headers.get('retry-after'));
      const cooldown = Math.ceil(Math.max(120, Number.isFinite(reset) ? reset : 0, Number.isFinite(delay) ? delay : 0));
      throw new CollectionError('rate_limited', Math.min(cooldown, 86400), res.status);
    }
    let data;
    try { data = object(await res.json()); } catch {
      if (res.status === 401) throw new CollectionError('authentication', 0, res.status);
      if (res.status === 403) throw new CollectionError('api_error', 0, res.status);
      throw new CollectionError(res.ok ? 'invalid_response' : 'upstream', 0, res.status);
    }
    // 403은 접근 거부도 포함한다. 메시지 원문은 버리고 문서화된 인증 오류 번호만 판정한다.
    const apiCodes = Array.isArray(data.errors)
      ? [...new Set(data.errors.map((error) => error?.code)
        .filter((code) => Number.isSafeInteger(code) && code >= 0 && code <= 65535))].slice(0, 8)
      : [];
    if (res.status === 401 || apiCodes.some((code) => code === 32 || code === 89)) {
      throw new CollectionError('authentication', 0, res.status, apiCodes);
    }
    if (res.status === 403) throw new CollectionError('api_error', 0, res.status, apiCodes);
    if (!res.ok) throw new CollectionError('upstream', 0, res.status, apiCodes);
    if (data.errors !== undefined && (!Array.isArray(data.errors) || data.errors.length > 0)) {
      throw new CollectionError('api_error', 0, res.status, apiCodes);
    }
    return data;
  }
  const data = await request(fleetPath, { user_ids: userIDs.join(','), only_spaces: 'true' });
  const users = object(data.users);
  const requested = new Set(userIDs);
  const spaceIDs = new Set();
  for (const [userID, value] of Object.entries(users)) {
    if (!requested.has(userID)) throw new CollectionError('unexpected_user');
    const user = object(value);
    if (user.spaces === undefined) continue;
    const spaces = object(user.spaces);
    if (spaces.live_content === undefined) continue;
    const audio = object(object(spaces.live_content).audiospace);
    if (typeof audio.broadcast_id !== 'string' || !/^[a-zA-Z0-9]{1,64}$/.test(audio.broadcast_id)) {
      throw new CollectionError('invalid_response');
    }
    spaceIDs.add(audio.broadcast_id);
  }
  const result = [];
  for (const spaceID of spaceIDs) {
    const response = await request(spacePath, {
      variables: JSON.stringify({ id: spaceID, isMetatagsQuery: true, withReplays: true, withListeners: true }),
      features: JSON.stringify(features),
    });
    const metadata = object(object(object(response.data).audioSpace).metadata);
    if (metadata.rest_id !== spaceID || !['Running', 'NotStarted', 'Ended', 'Canceled'].includes(metadata.state)) {
      throw new CollectionError('invalid_response');
    }
    if (metadata.state !== 'Running') continue;
    const creatorID = object(object(metadata.creator_results).result).rest_id;
    if (typeof creatorID !== 'string' || !/^[0-9]{1,20}$/.test(creatorID)) throw new CollectionError('invalid_response');
    if (!requested.has(creatorID)) continue;
    if (!Number.isSafeInteger(metadata.started_at) || metadata.started_at <= 0 ||
        typeof metadata.title !== 'string' || Buffer.byteLength(metadata.title) > 2048) {
      throw new CollectionError('invalid_response');
    }
    const startedAt = new Date(metadata.started_at);
    if (!Number.isFinite(startedAt.getTime())) throw new CollectionError('invalid_response');
    result.push({ space_id: spaceID, creator_id: creatorID,
      title: metadata.title.replace(/[\p{Cc}\p{Zl}\p{Zp}]/gu, ' ').trim(), started_at: startedAt.toISOString() });
  }
  return result;
}
