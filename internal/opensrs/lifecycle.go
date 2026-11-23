package opensrs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RenewResult struct {
	Domain    string     `json:"domain"`
	OrderID   string     `json:"order_id"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	AutoRenew bool       `json:"auto_renew"`
	Code      int        `json:"code"`
	Text      string     `json:"text"`
}

func (c *Client) GetExpireInfo(ctx context.Context, domain string) (expireYear int, expireAt *time.Time, autoRenew bool, err error) {
	domain = NormalizeDomain(domain)
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">GET</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="type">expire_action</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain))
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return 0, nil, false, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return 0, nil, false, fmt.Errorf("opensrs get expire %d: %s", code, text)
	}
	s := string(raw)
	expireStr := parseItemValue(s, "expiredate")
	if expireStr == "" {
		expireStr = parseItemValue(s, "registry_expiredate")
	}
	if expireStr != "" {
		if t, e := parseOpenSRSTime(expireStr); e == nil {
			expireAt = &t
			expireYear = t.Year()
		} else if len(expireStr) >= 4 {
			expireYear, _ = strconv.Atoi(expireStr[:4])
		}
	}
	ar := parseItemValue(s, "auto_renew")
	autoRenew = ar == "1" || strings.EqualFold(ar, "Y") || strings.EqualFold(ar, "true")
	if expireYear == 0 {
		return 0, expireAt, autoRenew, fmt.Errorf("could not parse expiration year from OpenSRS")
	}
	return expireYear, expireAt, autoRenew, nil
}

func (c *Client) Renew(ctx context.Context, domain string, years int, autoRenew bool) (RenewResult, error) {
	domain = NormalizeDomain(domain)
	if years < 1 {
		years = 1
	}
	if years > 10 {
		years = 10
	}
	expireYear, _, _, err := c.GetExpireInfo(ctx, domain)
	if err != nil {
		return RenewResult{}, err
	}
	ar := "0"
	if autoRenew {
		ar = "1"
	}
	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">RENEW</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="period">%d</item>
<item key="currentexpirationyear">%d</item>
<item key="auto_renew">%s</item>
<item key="handle">process</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain), years, expireYear, ar)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return RenewResult{}, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok {
		return RenewResult{}, fmt.Errorf("opensrs renew: unparseable response")
	}
	res := RenewResult{Domain: domain, Code: code, Text: text, AutoRenew: autoRenew}
	if code != 200 {
		return res, fmt.Errorf("opensrs renew %d: %s", code, text)
	}
	s := string(raw)
	res.OrderID = parseItemValue(s, "order_id")
	if res.OrderID == "" {
		res.OrderID = parseItemValue(s, "id")
	}
	expStr := parseItemValue(s, "registration expiration date")
	if expStr == "" {
		expStr = parseItemValue(s, "expiredate")
	}
	if t, e := parseOpenSRSTime(expStr); e == nil {
		res.ExpiresAt = &t
	} else {
		// fallback: add years to previous expire year end
		t := time.Date(expireYear+years, time.December, 31, 0, 0, 0, 0, time.UTC)
		res.ExpiresAt = &t
	}
	return res, nil
}

func (c *Client) SetAutoRenew(ctx context.Context, domain string, autoRenew bool) error {
	domain = NormalizeDomain(domain)
	ar := "0"
	if autoRenew {
		ar = "1"
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
<item key="data">expire_action</item>
<item key="affect_domains">0</item>
<item key="auto_renew">%s</item>
<item key="let_expire">0</item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`, xmlEscape(domain), ar)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return fmt.Errorf("opensrs set auto_renew %d: %s", code, text)
	}
	return nil
}

func (c *Client) UpdateContacts(ctx context.Context, domain string, contact ContactInfo) error {
	domain = NormalizeDomain(domain)
	contact = normalizeContact(contact)
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
<item key="data">contact_info</item>
<item key="affect_domains">0</item>
<item key="contact_set"><dt_assoc>
%s
%s
%s
%s
</dt_assoc></item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`,
		xmlEscape(domain),
		contactXML("owner", contact),
		contactXML("admin", contact),
		contactXML("billing", contact),
		contactXML("tech", contact),
	)
	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok || code != 200 {
		return fmt.Errorf("opensrs update contacts %d: %s", code, text)
	}
	return nil
}

func parseOpenSRSTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}