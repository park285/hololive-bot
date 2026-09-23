import { open } from 'node:fs/promises';
import { isAbsolute } from 'node:path';
import { createInterface } from 'node:readline/promises';
import { Writable } from 'node:stream';
import { validateCredentials } from './src/workflow.mjs';

// 최초 설정 전용입니다. 비밀은 TTY에서만 받고 기존 파일을 덮어쓰지 않습니다.
async function configure() {
  const [path, revisionText, ...extra] = process.argv.slice(2);
  const revision = Number(revisionText);
  if (!process.stdin.isTTY || !process.stdout.isTTY || !path || !isAbsolute(path)
    || !Number.isSafeInteger(revision) || revision < 1 || extra.length > 0) {
    throw new Error('invalid_invocation');
  }
  let hidden = false;
  const output = new Writable({
    write(chunk, encoding, callback) {
      if (hidden) callback(); else process.stdout.write(chunk, encoding, callback);
    },
  });
  const terminal = createInterface({ input: process.stdin, output, terminal: true, historySize: 0 });
  let password = '';
  try {
    const username = (await terminal.question('X 전용 계정 아이디(@ 제외): ')).trim();
    hidden = true;
    process.stdout.write('비밀번호(표시되지 않음): ');
    password = await terminal.question('');
    process.stdout.write('\n');
    const credentials = validateCredentials({ username, password });
    const raw = Buffer.from(`${JSON.stringify({ revision, ...credentials })}\n`);
    try {
      const file = await open(path, 'wx', 0o600);
      try { await file.writeFile(raw); await file.sync(); } finally { await file.close(); }
    } finally { raw.fill(0); credentials.password = ''; }
    process.stdout.write('보호된 계정 설정을 생성했습니다. 값은 출력하지 않습니다.\n');
  } finally {
    password = '';
    terminal.close();
  }
}

configure().catch(() => {
  process.stderr.write('계정 설정을 완료하지 못했습니다. TTY·경로·기존 파일 여부를 확인하십시오.\n');
  process.exitCode = 1;
});
