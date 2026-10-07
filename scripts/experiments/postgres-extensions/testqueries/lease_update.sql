\set job random(1, 1000)
BEGIN;
SELECT epoch FROM extension_benchmark.jobs WHERE id = :job FOR UPDATE;
UPDATE extension_benchmark.jobs SET epoch = epoch + 1, updated_at = now() WHERE id = :job;
COMMIT;
