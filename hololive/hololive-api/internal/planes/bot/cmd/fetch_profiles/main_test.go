package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := fn(req)
	if err != nil {
		return nil, fmt.Errorf("fn: %w", err)
	}

	return out, nil
}

func TestFetchProfileResponseRejectsNilHTTPResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil //nolint:nilnil // 응답과 오류가 모두 nil인 비정상 transport 재현이 이 테스트의 검증 대상이다.
	})}

	resp, err := fetchProfileResponse(t.Context(), client, "https://example.com/profile")
	if resp != nil && resp.Body != nil {
		defer closeBody(resp.Body)
	}

	if err == nil {
		t.Fatal("fetchProfileResponse() error = nil, want nil response error")
	}

	if resp != nil {
		t.Fatalf("fetchProfileResponse() response = %#v, want nil", resp)
	}

	if !strings.Contains(err.Error(), "failed to fetch URL") {
		t.Fatalf("fetchProfileResponse() error = %v, want fetch context", err)
	}
}

func TestFetchProfileParsesResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte(`
<html>
  <body>
    <section class="right_box">
      <h1>雪花ラミィ<span>Yukihana Lamy</span></h1>
      <p class="catch">Catch phrase</p>
      <p class="txt">Line 1<br>Line 2</p>
      <div class="t_sns"><a href="https://example.com">Example</a></div>
    </section>
    <section class="talent_data">
      <div class="table_box"><dl><dt>Birthday</dt><dd>November 15</dd></dl></div>
    </section>
  </body>
</html>`))
		if err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	profile, err := fetchProfile(t.Context(), server.Client(), server.URL, "Fallback Name", "yukihana-lamy")
	if err != nil {
		t.Fatalf("fetchProfile() error = %v", err)
	}

	if profile.EnglishName != "Yukihana Lamy" {
		t.Fatalf("EnglishName=%q", profile.EnglishName)
	}

	if profile.JapaneseName != "雪花ラミィ" {
		t.Fatalf("JapaneseName=%q", profile.JapaneseName)
	}

	if len(profile.SocialLinks) != 1 {
		t.Fatalf("len(SocialLinks)=%d", len(profile.SocialLinks))
	}

	if len(profile.DataEntries) != 1 {
		t.Fatalf("len(DataEntries)=%d", len(profile.DataEntries))
	}
}
