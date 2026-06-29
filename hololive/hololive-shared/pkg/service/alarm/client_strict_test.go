package alarm

import "testing"

func TestValidateAlarmServiceOriginRejectsQueryAndFragmentMarkers(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{
		"http://alarm?",
		"http://alarm#",
		"http://alarm#status",
		"http://alarm?ready=1",
	} {
		if err := validateAlarmServiceOrigin(baseURL); err == nil {
			t.Fatalf("validateAlarmServiceOrigin(%q) error = nil, want non-nil", baseURL)
		}
	}
}

func TestValidateAlarmServiceOriginAllowsBareOrigin(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{
		"http://alarm",
		"https://alarm:30007/",
	} {
		if err := validateAlarmServiceOrigin(baseURL); err != nil {
			t.Fatalf("validateAlarmServiceOrigin(%q) error = %v, want nil", baseURL, err)
		}
	}
}
