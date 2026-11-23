package opensrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0, 4096)
		buf := make([]byte, 1024)
		for {
			n, err := r.Body.Read(buf)
			if n > 0 {
				body = append(body, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		s := string(body)
		if !strings.Contains(s, "SW_REGISTER") || !strings.Contains(s, "testdomain.com") {
			t.Fatalf("bad body %s", s[:min(200, len(s))])
		}
		_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">Domain registration successfully completed</item>
<item key="id">999001</item>`))
	}))
	defer srv.Close()

	c := New("u", "k", srv.URL)
	c.http = srv.Client()
	res, err := c.Register(context.Background(), RegisterRequest{
		Domain: "testdomain.com",
		Years:  1,
		Contact: ContactInfo{
			FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com",
			Phone: "+91.9999999999", Address1: "1 St", City: "Bengaluru", State: "KA",
			PostalCode: "560001", Country: "IN",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OrderID != "999001" || !res.IsSuccess {
		t.Fatalf("%+v", res)
	}
}

func TestNormalizePhone(t *testing.T) {
	if got := normalizePhone("9876543210", "IN"); got != "+91.9876543210" {
		t.Fatal(got)
	}
	if got := normalizePhone("+1.4165550123", "US"); got != "+1.4165550123" {
		t.Fatal(got)
	}
	if got := normalizePhone("+919876543210", "IN"); got != "+91.9876543210" {
		t.Fatal(got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
