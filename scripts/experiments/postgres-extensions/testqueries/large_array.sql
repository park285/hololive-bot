\set first random(1, 3970)
SELECT id, status, history FROM extension_benchmark.heads
WHERE id >= :first AND id < :first + 30 ORDER BY id;
