package settings

import "testing"

func TestIrisRuntimeValidationAlwaysValidatesFileStatInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	if !LoadIrisRuntimeValidationConfig().ValidateFileStat {
		t.Fatal("production must always validate IRIS_BASE_URL_FILE stat")
	}
}
