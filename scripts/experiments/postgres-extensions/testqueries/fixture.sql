\set ON_ERROR_STOP on
CREATE SCHEMA extension_benchmark;
CREATE TABLE extension_benchmark.heads (
    id integer PRIMARY KEY,
    status text NOT NULL,
    history timestamptz[] NOT NULL
);
INSERT INTO extension_benchmark.heads
SELECT id, CASE WHEN id % 20 < 2 THEN 'LIVE' ELSE 'ENDED' END,
       ARRAY(SELECT '2026-09-01 UTC'::timestamptz + slot * interval '1 minute'
             FROM generate_series(1, CASE id % 4 WHEN 0 THEN 17000 WHEN 1 THEN 1000 ELSE 0 END) slot)
FROM generate_series(1, 4000) AS generated(id);

CREATE TABLE extension_benchmark.targets (
    generation integer NOT NULL,
    subject_key integer NOT NULL,
    kind integer NOT NULL,
    poll_interval_ms integer NOT NULL,
    priority integer NOT NULL,
    enabled boolean NOT NULL,
    PRIMARY KEY (generation, subject_key, kind)
);
INSERT INTO extension_benchmark.targets
SELECT 1, id / 3, id % 3, 120000, id % 5, true FROM generate_series(1, 600) AS generated(id);

CREATE TABLE extension_benchmark.jobs (
    id integer PRIMARY KEY,
    epoch bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO extension_benchmark.jobs (id) SELECT generate_series(1, 1000);
VACUUM (ANALYZE);
