package opensrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSetNameservers(t *testing.T) {
	var sawSet bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		if containsAction(body, "ADVANCED_UPDATE_NAMESERVERS") && containsAction(body, "assign") {
			sawSet = true
		}
		_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">Command Successful</item>
<item key="nameserver_list"><dt_array>
<item key="0"><dt_assoc><item key="name">ns1.example.net</item></dt_assoc></item>
<item key="1"><dt_assoc><item key="name">ns2.example.net</item></dt_assoc></item>
</dt_array></item>`))
	}))
	defer srv.Close()

	c := New("u", "k", srv.URL)
	c.http = srv.Client()
	ns, err := c.GetNameservers(context.Background(), "foo.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 {
		t.Fatalf("%v", ns)
	}
	if err := c.SetNameservers(context.Background(), "foo.com", []string{"ns1.example.net", "ns2.example.net"}); err != nil {
		t.Fatal(err)
	}
	if !sawSet {
		t.Fatal("expected advanced_update_nameservers assign call")
	}
}

func TestBuildRecordsXML(t *testing.T) {
	xml := buildRecordsXML([]DNSRecord{{Type: "A", Host: "www", Value: "1.2.3.4"}})
	if !containsAction(xml, "ip_address") || !containsAction(xml, "www") {
		t.Fatal(xml)
	}
}

func readBody(r *http.Request) string {
	b := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			b = append(b, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(b)
}

func containsAction(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
