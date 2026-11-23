package opensrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenewFlow(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		n++
		if strings.Contains(body, "expire_action") {
			_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">OK</item>
<item key="expiredate">2027-11-23 00:00:00</item>
<item key="auto_renew">1</item>`))
			return
		}
		if !strings.Contains(body, "RENEW") || !strings.Contains(body, "currentexpirationyear\">2027") {
			t.Fatalf("bad renew body %s", body[:min(300, len(body))])
		}
		_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">Command completed successfully</item>
<item key="order_id">555</item>
<item key="registration expiration date">2028-11-23 00:00:00</item>`))
	}))
	defer srv.Close()
	c := New("u", "k", srv.URL)
	c.http = srv.Client()
	res, err := c.Renew(context.Background(), "foo.com", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.OrderID != "555" || res.ExpiresAt == nil {
		t.Fatalf("%+v", res)
	}
}

func TestParseSuggestResults(t *testing.T) {
	raw := `
<item key="lookup"><dt_assoc>
<item key="items"><dt_array>
<item key="0"><dt_assoc>
<item key="domain">brand.com</item>
<item key="status">available</item>
</dt_assoc></item>
</dt_array></item>
</dt_assoc></item>
<item key="suggestion"><dt_assoc>
<item key="items"><dt_array>
<item key="0"><dt_assoc>
<item key="domain">getbrand.com</item>
<item key="status">available</item>
</dt_assoc></item>
</dt_array></item>
</dt_assoc></item>`
	got := parseSuggestResults(raw)
	if len(got) < 2 {
		t.Fatalf("%+v", got)
	}
}
