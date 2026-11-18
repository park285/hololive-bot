import { boundedFetch, collectSpaces, CollectionError, failureReport, validateCookies } from './client.mjs';

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
