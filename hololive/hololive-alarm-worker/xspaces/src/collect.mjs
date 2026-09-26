import { boundedFetch, collectSpaces, CollectionError, failureReport, validateCookies } from './client.mjs';

// 예외 원문을 남기지 않으므로 운영 로그에서 원인 범위를 좁히도록 실패한 단계를 함께 보고한다.
let stage = 'input';
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
  stage = 'library';
  const { ClientTransaction, fetchXDocument } = await import('x-client-transaction-id');
  stage = 'app_shell';
  const document = await fetchXDocument();
  stage = 'transaction';
  const transaction = await ClientTransaction.create(document);
  stage = 'collect';
  const spaces = await collectSpaces(userIDs, cookies, transaction, globalThis.fetch);
  process.stdout.write(JSON.stringify({ spaces }));
} catch (error) {
  process.stdout.write(JSON.stringify(failureReport(error, stage)));
  process.exitCode = 1;
}
