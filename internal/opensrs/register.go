package opensrs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ContactInfo struct {
	FirstName    string
	LastName     string
	Organization string
	Email        string
	Phone        string
	Address1     string
	Address2     string
	City         string
	State        string
	PostalCode   string
	Country      string
}

type RegisterRequest struct {
	Domain     string
	Years      int
	AutoRenew  bool
	Locked     bool
	Username   string
	Password   string
	Contact    ContactInfo
	Nameservers []string
}

type RegisterResult struct {
	Domain      string
	OrderID     string
	Code        int
	Text        string
	IsSuccess   bool
}

func (c *Client) Register(ctx context.Context, req RegisterRequest) (RegisterResult, error) {
	domain := NormalizeDomain(req.Domain)
	if domain == "" {
		return RegisterResult{}, fmt.Errorf("domain required")
	}
	years := req.Years
	if years < 1 {
		years = 1
	}
	if years > 10 {
		years = 10
	}
	ns := req.Nameservers
	if len(ns) < 2 {
		ns = []string{"ns1.systemdns.com", "ns2.systemdns.com"}
	}
	user := sanitizeUsername(req.Username)
	if user == "" {
		user = "namepiq" + fmt.Sprintf("%d", time.Now().Unix()%100000)
	}
	pass := req.Password
	if len(pass) < 8 {
		pass = "Np" + fmt.Sprintf("%d", time.Now().Unix()) + "!"
	}
	contact := normalizeContact(req.Contact)
	autoRenew := "0"
	if req.AutoRenew {
		autoRenew = "1"
	}
	lock := "0"
	if req.Locked {
		lock = "1"
	}

	xmlBody := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header><version>0.9</version></header>
<body><data_block><dt_assoc>
<item key="protocol">XCP</item>
<item key="action">SW_REGISTER</item>
<item key="object">DOMAIN</item>
<item key="attributes"><dt_assoc>
<item key="domain">%s</item>
<item key="period">%d</item>
<item key="reg_type">new</item>
<item key="handle">process</item>
<item key="reg_username">%s</item>
<item key="reg_password">%s</item>
<item key="auto_renew">%s</item>
<item key="f_lock_domain">%s</item>
<item key="custom_nameservers">1</item>
<item key="custom_tech_contact">1</item>
<item key="dns_template">*blank*</item>
<item key="contact_set"><dt_assoc>
%s
%s
%s
%s
</dt_assoc></item>
<item key="nameserver_list"><dt_array>
%s
</dt_array></item>
</dt_assoc></item>
</dt_assoc></data_block></body>
</OPS_envelope>`,
		xmlEscape(domain),
		years,
		xmlEscape(user),
		xmlEscape(pass),
		autoRenew,
		lock,
		contactXML("owner", contact),
		contactXML("admin", contact),
		contactXML("billing", contact),
		contactXML("tech", contact),
		nameserverXML(ns),
	)

	raw, err := c.post(ctx, xmlBody)
	if err != nil {
		return RegisterResult{}, err
	}
	code, text, ok := parseReplyMeta(raw)
	if !ok {
		return RegisterResult{}, fmt.Errorf("opensrs register: unparseable response")
	}
	res := RegisterResult{
		Domain:    domain,
		Code:      code,
		Text:      text,
		IsSuccess: code == 200 || code == 210,
		OrderID:   parseItemValue(string(raw), "id"),
	}
	if !res.IsSuccess {
		return res, fmt.Errorf("opensrs register %d: %s", code, text)
	}
	return res, nil
}

func contactXML(role string, c ContactInfo) string {
	org := c.Organization
	if org == "" {
		org = c.FirstName + " " + c.LastName
	}
	return fmt.Sprintf(`<item key="%s"><dt_assoc>
<item key="first_name">%s</item>
<item key="last_name">%s</item>
<item key="org_name">%s</item>
<item key="email">%s</item>
<item key="phone">%s</item>
<item key="address1">%s</item>
<item key="address2">%s</item>
<item key="city">%s</item>
<item key="state">%s</item>
<item key="postal_code">%s</item>
<item key="country">%s</item>
</dt_assoc></item>`,
		role,
		xmlEscape(c.FirstName),
		xmlEscape(c.LastName),
		xmlEscape(org),
		xmlEscape(c.Email),
		xmlEscape(c.Phone),
		xmlEscape(c.Address1),
		xmlEscape(c.Address2),
		xmlEscape(c.City),
		xmlEscape(c.State),
		xmlEscape(c.PostalCode),
		xmlEscape(strings.ToUpper(c.Country)),
	)
}

func nameserverXML(ns []string) string {
	var b strings.Builder
	for i, n := range ns {
		b.WriteString(fmt.Sprintf(`<item key="%d"><dt_assoc>
<item key="name">%s</item>
<item key="sortorder">%d</item>
</dt_assoc></item>`, i, xmlEscape(strings.ToLower(strings.TrimSpace(n))), i+1))
	}
	return b.String()
}

func normalizeContact(c ContactInfo) ContactInfo {
	c.FirstName = strings.TrimSpace(c.FirstName)
	c.LastName = strings.TrimSpace(c.LastName)
	c.Organization = strings.TrimSpace(c.Organization)
	c.Email = strings.TrimSpace(strings.ToLower(c.Email))
	c.Phone = normalizePhone(c.Phone, c.Country)
	c.Address1 = strings.TrimSpace(c.Address1)
	c.Address2 = strings.TrimSpace(c.Address2)
	c.City = strings.TrimSpace(c.City)
	c.State = strings.TrimSpace(c.State)
	c.PostalCode = strings.TrimSpace(c.PostalCode)
	c.Country = strings.ToUpper(strings.TrimSpace(c.Country))
	if c.Country == "" {
		c.Country = "IN"
	}
	if c.FirstName == "" {
		c.FirstName = "Namepiq"
	}
	if c.LastName == "" {
		c.LastName = "User"
	}
	if c.Address1 == "" {
		c.Address1 = "Address"
	}
	if c.City == "" {
		c.City = "Bengaluru"
	}
	if c.State == "" {
		c.State = "KA"
	}
	if c.PostalCode == "" {
		c.PostalCode = "560001"
	}
	return c
}

var reDigits = regexp.MustCompile(`\D+`)

func normalizePhone(phone, country string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+") && strings.Contains(phone, ".") {
		return phone
	}
	digits := reDigits.ReplaceAllString(phone, "")
	cc := "91"
	switch strings.ToUpper(country) {
	case "US", "CA":
		cc = "1"
	case "GB", "UK":
		cc = "44"
	case "AU":
		cc = "61"
	case "SG":
		cc = "65"
	case "AE":
		cc = "971"
	case "JP":
		cc = "81"
	case "DE":
		cc = "49"
	case "FR":
		cc = "33"
	case "ES":
		cc = "34"
	case "IN", "":
		cc = "91"
	}
	if digits == "" {
		digits = "9999999999"
	}
	// E.164 (+919876543210) → OpenSRS (+91.9876543210)
	if strings.HasPrefix(phone, "+") && strings.HasPrefix(digits, cc) && len(digits) > len(cc)+5 {
		return "+" + cc + "." + digits[len(cc):]
	}
	// strip leading country code if already present
	if strings.HasPrefix(digits, cc) && len(digits) > len(cc)+6 {
		digits = digits[len(cc):]
	}
	return "+" + cc + "." + digits
}

func sanitizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

var reItemKey = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)<item key="` + regexp.QuoteMeta(key) + `">([^<]*)</item>`)
}

func parseItemValue(s, key string) string {
	m := reItemKey(key).FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
