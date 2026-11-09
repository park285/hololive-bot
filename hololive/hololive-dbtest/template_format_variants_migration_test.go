package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	templateFormatCustomKey       = "CMD_HELP"
	templateFormatOverrideChannel = "format-override"
)

func TestTemplateFormatConstraints(t *testing.T) {
	t.Parallel()
	pool := NewPool(t)
	_, err := pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body,format_variant) VALUES($1,$2,'본문','plain')`, templateFormatCustomKey, templateFormatOverrideChannel)
	require.NoError(t, err)
	assertTemplateFormatConstraints(t, pool)
}

func assertTemplateFormatConstraints(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO notification_templates(template_key,body,format_variant) VALUES($1,'중복','richtext')`, templateFormatCustomKey)
	require.ErrorContains(t, err, "ux_notification_templates_default_format")

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant) VALUES($1,$2,'중복','plain')`,
		templateFormatCustomKey, templateFormatOverrideChannel)
	require.ErrorContains(t, err, "ux_notification_templates_channel_format")

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant) VALUES($1,'other','본문','markdown')`, templateFormatCustomKey)
	require.ErrorContains(t, err, "notification_templates_format_variant_check")

	var version int64

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body) VALUES($1,'other-format','본문')`, templateFormatCustomKey)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `UPDATE notification_templates SET format_variant='richtext'
WHERE template_key=$1 AND channel_id='other-format' RETURNING row_version`, templateFormatCustomKey).Scan(&version))
	require.Equal(t, int64(2), version)
}
