package runtime

import "testing"

func TestRuntimeSQLAssetsLoad(t *testing.T) {
	t.Parallel()
	if mustSQL("notification_channel_ids.sql") == "" || mustSQL("operational_channel_ids.sql") == "" {
		t.Fatal("youtube plane roster SQL assets must load")
	}
}
