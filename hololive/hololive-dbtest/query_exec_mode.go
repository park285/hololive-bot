package dbtest

import "github.com/jackc/pgx/v5"

var productionQueryExecMode = pgx.QueryExecModeCacheStatement

const productionComposeQueryExecModeName = "cache_statement"
