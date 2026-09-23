import { boundedFetch, collectSpaces, CollectionError, validateCookies } from './client.mjs';

try {
  let input = '';
  for await (const chunk of process.stdin) {
    input += chunk;
    if (input.length > 8192) throw new CollectionError('invalid_targets');
  }
  const request = JSON.parse(input);
  const userIDs = request.user_ids;
  const cookies = validateCookies(request.cookies);
  // 전용 단발 프로세스에서만 교체한다. 의존성의 웹 자료 요청도 같은 경계를 따른다.
  globalThis.fetch = boundedFetch(globalThis.fetch);
  const { ClientTransaction, fetchXDocument } = await import('x-client-transaction-id');
  const transaction = await ClientTransaction.create(await fetchXDocument());
  const spaces = await collectSpaces(userIDs, cookies, transaction, globalThis.fetch);
  process.stdout.write(JSON.stringify({ spaces }));
} catch (error) {
  // 라이브러리 오류의 메시지·stack·요청 객체에는 인증 정보가 포함될 수 있다.
  const code = error instanceof CollectionError ? error.code : 'collector_failed';
  const cooldownSeconds = error instanceof CollectionError ? error.cooldownSeconds : 0;
  const httpStatus = error instanceof CollectionError ? error.httpStatus : 0;
  const apiCodes = error instanceof CollectionError ? error.apiCodes : [];
  process.stdout.write(JSON.stringify({ error: code, cooldown_seconds: cooldownSeconds, http_status: httpStatus, api_codes: apiCodes }));
  process.exitCode = 1;
}
