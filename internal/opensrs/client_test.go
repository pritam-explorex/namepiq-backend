package opensrs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignStable(t *testing.T) {
	xml := "<xml/>"
	key := "secret"
	a := Sign(xml, key)
	b := Sign(xml, key)
	if a != b || len(a) != 32 {
		t.Fatalf("bad signature %q", a)
	}
	if Sign(xml, "other") == a {
		t.Fatal("different key should change signature")
	}
}

func TestNormalizeAndExpand(t *testing.T) {
	if NormalizeDomain("HTTPS://WWW.Example.COM/path") != "example.com" {
		t.Fatal(NormalizeDomain("HTTPS://WWW.Example.COM/path"))
	}
	c := ExpandCandidates("acme")
	if len(c) < 3 || c[0] != "acme.com" {
		t.Fatalf("%v", c)
	}
	fq := ExpandCandidates("acme.io")
	if len(fq) < 3 || fq[0] != "acme.io" {
		t.Fatalf("preferred tld first: %v", fq)
	}
	if ExpandCandidates("foo.co.uk")[0] != "foo.co.uk" || len(ExpandCandidates("foo.co.uk")) != 1 {
		t.Fatalf("multi-dot exact only: %v", ExpandCandidates("foo.co.uk"))
	}
}

func TestLookupAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Username") == "" || r.Header.Get("X-Signature") == "" {
			t.Fatal("missing auth headers")
		}
		_, _ = w.Write([]byte(`
<OPS_envelope><body><data_block><dt_assoc>
<item key="response_code">210</item>
<item key="response_text">Domain available</item>
</dt_assoc></data_block></body></OPS_envelope>`))
	}))
	defer srv.Close()

	c := New("user", "key", srv.URL)
	c.http = srv.Client()
	res, err := c.Lookup(context.Background(), "free-example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available || res.Code != 210 {
		t.Fatalf("%+v", res)
	}
}

func TestLookupTaken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
<item key="response_code">211</item>
<item key="response_text">Domain taken</item>`))
	}))
	defer srv.Close()
	c := New("user", "key", srv.URL)
	c.http = srv.Client()
	res, err := c.Lookup(context.Background(), "google.com")
	if err != nil {
		t.Fatal(err)
	}
	if res.Available {
		t.Fatal("expected taken")
	}
}

func TestGetPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
<item key="response_code">200</item>
<item key="response_text">Command Successful</item>
<item key="price">11.81</item>
<item key="is_registry_premium">0</item>`))
	}))
	defer srv.Close()
	c := New("user", "key", srv.URL)
	c.http = srv.Client()
	res, err := c.GetPrice(context.Background(), "example.com", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.PriceUSD < 11.8 || res.PriceUSD > 11.82 {
		t.Fatalf("%v", res.PriceUSD)
	}
}

func TestLookupXMLEscapes(t *testing.T) {
	xml := lookupXML(`a&b.com`)
	if !strings.Contains(xml, "a&amp;b.com") {
		t.Fatal(xml)
	}
}
