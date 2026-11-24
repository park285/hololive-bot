import { createInterface } from 'node:readline';
import { boundedFetch, collectSpaces, CollectionError, failureReport } from './client.mjs';

globalThis.fetch = boundedFetch(globalThis.fetch);
let library;

async function collect(line) {
  let stage = 'input';
  try {
    if (line.length > 8192) throw new CollectionError('invalid_targets');
    const request = JSON.parse(line);
    stage = 'library';
    library ??= await import('x-client-transaction-id');
    stage = 'app_shell';
    const document = await library.fetchXDocument();
    stage = 'transaction';
    const transaction = await library.ClientTransaction.create(document);
    stage = 'collect';
    return { spaces: await collectSpaces(request.user_ids, request.cookies, transaction, globalThis.fetch) };
  } catch (error) {
    return failureReport(error, stage);
  }
}

for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  process.stdout.write(`${JSON.stringify(await collect(line))}\n`);
}
