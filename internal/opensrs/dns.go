package opensrs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type DNSRecord struct {
	Type      string `json:"type"`
	Host      string `json:"host"`
	Value     string `json:"value"`
	Priority  string `json:"priority,omitempty"`
	Weight    string `json:"weight,omitempty"`
	Port      string `json:"port,omitempty"`
}

func (c *Client) GetNameservers(ctx context.Context, domain string) ([]string, error) {
	domain = NormalizeDomain(domain)
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">GET</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="type">nameservers</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain))
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return nil, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return nil, fmt.Errorf("opensrs get nameservers %d: %s", code, text)
	}
	return parseNameserverList(string(raw)), nil
}

func (c *Client) SetDomainLock(ctx context.Context, domain string, locked bool) error {
	domain = NormalizeDomain(domain)
	state := "0"
	if locked {
		state = "1"
	}
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">MODIFY</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="data">status</item>
<item key="lock_state">%s</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain), state)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return fmt.Errorf("opensrs set lock %d: %s", code, text)
	}
	return nil
}

func (c *Client) SetNameservers(ctx context.Context, domain string, nameservers []string) error {
	domain = NormalizeDomain(domain)
	clean := make([]string, 0, len(nameservers))
	seen := map[string]bool{}
	for _, ns := range nameservers {
		ns = strings.TrimSpace(strings.ToLower(ns))
		ns = strings.TrimSuffix(ns, ".")
		if ns == "" || seen[ns] {
			continue
		}
		if !strings.Contains(ns, ".") {
			return fmt.Errorf("invalid nameserver hostname: %s", ns)
		}
		seen[ns] = true
		clean = append(clean, ns)
	}
	if len(clean) < 2 {
		return fmt.Errorf("at least two nameservers required")
	}
	var nsItems strings.Builder
	for i, ns := range clean {
		nsItems.WriteString(fmt.Sprintf(`<item key="%d">%s</item>`, i, xmlEscape(ns)))
	}
	// OpenSRS: advanced_update_nameservers + op_type=assign (not MODIFY/assign_ns).
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">ADVANCED_UPDATE_NAMESERVERS</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="op_type">assign</item>
<item key="assign_ns"><dt_array>%s</dt_array></item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain), nsItems.String())

	assign := func() (int, string, error) {
		raw, err := c.post(ctx, xmlBody)
		if err != nil {
			return 0, "", err
		}
		code, text, ok := parseReplyMeta(raw)
		if !ok {
			return 0, "", fmt.Errorf("unparseable response")
		}
		return code, text, nil
	}

	code, text, err := assign()
	if err != nil {
		return err
	}
	// Locked domains often return 405; unlock, retry, re-lock.
	if code != 200 && (code == 405 || strings.Contains(strings.ToLower(text), "prohibit") || strings.Contains(strings.ToLower(text), "lock")) {
		_ = c.SetDomainLock(ctx, domain, false)
		code, text, err = assign()
		_ = c.SetDomainLock(ctx, domain, true)
		if err != nil {
			return err
		}
	}
	if code != 200 {
		return fmt.Errorf("opensrs set nameservers %d: %s", code, text)
	}
	return nil
}

type DNSZone struct {
	Records        []DNSRecord `json:"records"`
	NameserversOK  bool        `json:"nameservers_ok"`
	Enabled        bool        `json:"enabled"`
}

var SystemDNSNameservers = []string{
	"ns1.systemdns.com",
	"ns2.systemdns.com",
	"ns3.systemdns.com",
}

func (c *Client) GetDNSZone(ctx context.Context, domain string) (DNSZone, error) {
	domain = NormalizeDomain(domain)
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">GET_DNS_ZONE</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain))
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return DNSZone{}, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return DNSZone{}, fmt.Errorf("opensrs get_dns_zone %d: %s", code, text)
	}
	s := string(raw)
	nsOK := parseItemValue(s, "nameservers_ok") == "1"
	recs := parseDNSRecords(s)
	return DNSZone{Records: recs, NameserversOK: nsOK, Enabled: true}, nil
}

func (c *Client) CreateDNSZone(ctx context.Context, domain string, records []DNSRecord) (DNSZone, error) {
	domain = NormalizeDomain(domain)
	recordsXML := buildRecordsXML(records)
	attrs := fmt.Sprintf(`<item key="domain">%s</item>`, xmlEscape(domain))
	if len(records) > 0 {
		attrs += fmt.Sprintf(`<item key="records">%s</item>`, recordsXML)
	}
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">CREATE_DNS_ZONE</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
%s
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, attrs)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return DNSZone{}, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return DNSZone{}, fmt.Errorf("opensrs create_dns_zone %d: %s", code, text)
	}
	s := string(raw)
	nsOK := parseItemValue(s, "nameservers_ok") == "1"
	recs := parseDNSRecords(s)
	if len(recs) == 0 {
		recs = records
	}
	return DNSZone{Records: recs, NameserversOK: nsOK, Enabled: true}, nil
}

func (c *Client) SetDNSZone(ctx context.Context, domain string, records []DNSRecord) (DNSZone, error) {
	domain = NormalizeDomain(domain)
	recordsXML := buildRecordsXML(records)
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">SET_DNS_ZONE</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="records">%s</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain), recordsXML)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return DNSZone{}, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return DNSZone{}, fmt.Errorf("opensrs set_dns_zone %d: %s", code, text)
	}
	s := string(raw)
	nsOK := parseItemValue(s, "nameservers_ok") == "1"
	return DNSZone{Records: records, NameserversOK: nsOK, Enabled: true}, nil
}

// EnableManagedDNS creates the OpenSRS DNS zone (if needed) and points the domain at SystemDNS.
func (c *Client) EnableManagedDNS(ctx context.Context, domain string) (DNSZone, error) {
	domain = NormalizeDomain(domain)
	zone, err := c.GetDNSZone(ctx, domain)
	if err != nil {
		zone, err = c.CreateDNSZone(ctx, domain, nil)
		if err != nil {
			return DNSZone{}, err
		}
	}
	if err := c.SetNameservers(ctx, domain, SystemDNSNameservers); err != nil {
		// Zone may still be usable; surface NS error but keep zone
		zone.NameserversOK = false
		return zone, fmt.Errorf("dns zone ready, but nameserver update failed: %w", err)
	}
	zone.NameserversOK = true
	zone.Enabled = true
	return zone, nil
}

var reNSName = regexp.MustCompile(`(?i)<item key="name">([^<]+)</item>`)

func parseNameserverList(s string) []string {
	// Prefer nameserver_list section if present; otherwise collect name items carefully.
	idx := strings.Index(strings.ToLower(s), `key="nameserver_list"`)
	chunk := s
	if idx >= 0 {
		chunk = s[idx:]
	}
	matches := reNSName.FindAllStringSubmatch(chunk, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		ns := strings.ToLower(strings.TrimSpace(m[1]))
		if ns == "" || ns == "all" || seen[ns] {
			continue
		}
		if !strings.Contains(ns, ".") {
			continue
		}
		seen[ns] = true
		out = append(out, ns)
	}
	return out
}

func parseDNSRecords(s string) []DNSRecord {
	var out []DNSRecord
	// Lightweight extraction: look for common record blocks.
	out = append(out, extractTypedRecords(s, "A", "ip_address")...)
	out = append(out, extractTypedRecords(s, "AAAA", "ipv6_address")...)
	out = append(out, extractTypedRecords(s, "CNAME", "hostname")...)
	out = append(out, extractTypedRecords(s, "MX", "hostname")...)
	out = append(out, extractTypedRecords(s, "TXT", "text")...)
	return out
}

func extractTypedRecords(s, typ, valueKey string) []DNSRecord {
	lower := strings.ToLower(s)
	needle := `key="` + strings.ToLower(typ) + `"`
	idx := strings.Index(lower, needle)
	if idx < 0 {
		return nil
	}
	chunk := s[idx:]
	// Limit chunk to a reasonable size
	if len(chunk) > 8000 {
		chunk = chunk[:8000]
	}
	reSub := regexp.MustCompile(`(?i)<item key="subdomain">([^<]*)</item>`)
	reVal := regexp.MustCompile(`(?i)<item key="` + regexp.QuoteMeta(valueKey) + `">([^<]*)</item>`)
	rePri := regexp.MustCompile(`(?i)<item key="priority">([^<]*)</item>`)
	subs := reSub.FindAllStringSubmatch(chunk, -1)
	vals := reVal.FindAllStringSubmatch(chunk, -1)
	pris := rePri.FindAllStringSubmatch(chunk, -1)
	n := len(vals)
	if len(subs) < n {
		n = len(subs)
	}
	var out []DNSRecord
	for i := 0; i < n; i++ {
		rec := DNSRecord{
			Type:  typ,
			Host:  strings.TrimSpace(subs[i][1]),
			Value: strings.TrimSpace(vals[i][1]),
		}
		if typ == "MX" && i < len(pris) {
			rec.Priority = strings.TrimSpace(pris[i][1])
		}
		out = append(out, rec)
	}
	return out
}

func buildRecordsXML(records []DNSRecord) string {
	grouped := map[string][]DNSRecord{}
	for _, r := range records {
		t := strings.ToUpper(strings.TrimSpace(r.Type))
		grouped[t] = append(grouped[t], r)
	}
	var b strings.Builder
	b.WriteString(`<dt_assoc>`)
	for typ, list := range grouped {
		b.WriteString(`<item key="` + typ + `"><dt_array>`)
		for i, r := range list {
			b.WriteString(fmt.Sprintf(`<item key="%d"><dt_assoc>`, i))
			b.WriteString(`<item key="subdomain">` + xmlEscape(r.Host) + `</item>`)
			switch typ {
			case "A":
				b.WriteString(`<item key="ip_address">` + xmlEscape(r.Value) + `</item>`)
			case "AAAA":
				b.WriteString(`<item key="ipv6_address">` + xmlEscape(r.Value) + `</item>`)
			case "CNAME", "MX":
				b.WriteString(`<item key="hostname">` + xmlEscape(r.Value) + `</item>`)
				if typ == "MX" {
					pri := r.Priority
					if pri == "" {
						pri = "10"
					}
					b.WriteString(`<item key="priority">` + xmlEscape(pri) + `</item>`)
				}
			case "TXT":
				b.WriteString(`<item key="text">` + xmlEscape(r.Value) + `</item>`)
			}
			b.WriteString(`</dt_assoc></item>`)
		}
		b.WriteString(`</dt_array></item>`)
	}
	b.WriteString(`</dt_assoc>`)
	return b.String()
}
