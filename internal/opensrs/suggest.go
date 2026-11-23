package opensrs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type SuggestResult struct {
	Domain    string `json:"domain"`
	Status    string `json:"status"` // available, taken, etc.
	Service   string `json:"service"` // lookup | suggestion | premium
}

func (c *Client) NameSuggest(ctx context.Context, query string, tlds []string) ([]SuggestResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("searchstring required")
	}
	if len(tlds) == 0 {
		tlds = []string{".com", ".net", ".org", ".in", ".io", ".ai", ".co"}
	}
	var tldItems strings.Builder
	for i, t := range tlds {
		t = strings.ToLower(strings.TrimSpace(t))
		if !strings.HasPrefix(t, ".") {
			t = "." + t
		}
		tldItems.WriteString(fmt.Sprintf(`<item key="%d">%s</item>`, i, xmlEscape(t)))
	}
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">NAME_SUGGEST</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="searchstring">%s</item>
<item key="services"><dt_array>
<item key="0">lookup</item>
<item key="1">suggestion</item>
</dt_array></item>
<item key="tlds"><dt_array>%s</dt_array></item>
<item key="maximum">20</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(query), tldItems.String())

	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return nil, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || (code != 200 && code != 210) {
		return nil, fmt.Errorf("opensrs name_suggest %d: %s", code, text)
	}
	return parseSuggestResults(string(raw)), nil
}

var (
	reSuggestDomain = regexp.MustCompile(`(?i)<item key="domain">([^<]+)</item>`)
	reSuggestStatus = regexp.MustCompile(`(?i)<item key="status">([^<]+)</item>`)
)

func parseSuggestResults(s string) []SuggestResult {
	lower := strings.ToLower(s)
	sections := []struct {
		key string
		svc string
	}{
		{`key="lookup"`, "lookup"},
		{`key="suggestion"`, "suggestion"},
		{`key="premium"`, "premium"},
	}
	var out []SuggestResult
	seen := map[string]bool{}
	for _, sec := range sections {
		idx := strings.Index(lower, sec.key)
		if idx < 0 {
			continue
		}
		chunk := s[idx:]
		if len(chunk) > 12000 {
			chunk = chunk[:12000]
		}
		// Split on domain items roughly pairing with following status
		domains := reSuggestDomain.FindAllStringSubmatchIndex(chunk, -1)
		for _, loc := range domains {
			domain := strings.ToLower(strings.TrimSpace(chunk[loc[2]:loc[3]]))
			if domain == "" || seen[domain] {
				continue
			}
			status := "unknown"
			tail := chunk[loc[1]:]
			if len(tail) > 400 {
				tail = tail[:400]
			}
			if m := reSuggestStatus.FindStringSubmatch(tail); len(m) > 1 {
				status = strings.ToLower(strings.TrimSpace(m[1]))
			}
			seen[domain] = true
			out = append(out, SuggestResult{Domain: domain, Status: status, Service: sec.svc})
		}
	}
	return out
}
