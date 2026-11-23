package opensrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNameSuggest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		if !strings.Contains(body, "NAME_SUGGEST") || !strings.Contains(body, "acme") {
			t.Fatalf("%s", body[:min(200, len(body))])
		}
		_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">OK</item>
<item key="lookup"><dt_assoc><item key="items"><dt_array>
<item key="0"><dt_assoc>
<item key="domain">acme.com</item>
<item key="status">taken</item>
</dt_assoc></item>
</dt_array></item></dt_assoc></item>
<item key="suggestion"><dt_assoc><item key="items"><dt_array>
<item key="0"><dt_assoc>
<item key="domain">acmehq.com</item>
<item key="status">available</item>
</dt_assoc></item>
</dt_array></item></dt_assoc></item>`))
	}))
	defer srv.Close()
	c := New("u", "k", srv.URL)
	c.http = srv.Client()
	res, err := c.NameSuggest(context.Background(), "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 2 {
		t.Fatalf("%+v", res)
	}
}
