package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchProfileParsesResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
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
	}))
	t.Cleanup(server.Close)

	profile, err := fetchProfile(context.Background(), server.Client(), server.URL, "Fallback Name", "yukihana-lamy")
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
