package tools

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Blocklist is the sites an administrator has ruled out: no search returns
// them and web_fetch will not read them.
//
// A rule is a domain (`example.com`, covering its subdomains), a wildcard
// (`*.example.com`, the subdomains only), or a regular expression between
// slashes (`/\.pdf$/`) matched against the whole URL.
type Blocklist struct {
	domains    []string // match the host and its subdomains
	subdomains []string // match subdomains only
	patterns   []*regexp.Regexp
}

// ParseBlocklist reads rules, one per entry. Blank entries are skipped; a
// malformed one is an error naming it, so the settings page can point at it.
func ParseBlocklist(rules []string) (Blocklist, error) {
	var b Blocklist
	for _, raw := range rules {
		rule := strings.TrimSpace(raw)
		switch {
		case rule == "":
			continue
		case len(rule) > 2 && strings.HasPrefix(rule, "/") && strings.HasSuffix(rule, "/"):
			pattern, err := regexp.Compile(rule[1 : len(rule)-1])
			if err != nil {
				return Blocklist{}, fmt.Errorf("%q is not a valid regular expression", rule)
			}
			b.patterns = append(b.patterns, pattern)
		case strings.HasPrefix(rule, "*."):
			host := hostOf(rule[2:])
			if host == "" {
				return Blocklist{}, fmt.Errorf("%q is not a domain", rule)
			}
			b.subdomains = append(b.subdomains, host)
		default:
			host := hostOf(rule)
			if host == "" {
				return Blocklist{}, fmt.Errorf("%q is not a domain", rule)
			}
			b.domains = append(b.domains, host)
		}
	}
	return b, nil
}

// hostOf takes a domain, or a URL pasted in its place, to a bare lowercase
// host. Empty when there is none.
func hostOf(rule string) string {
	rule = strings.ToLower(strings.TrimSpace(rule))
	if !strings.Contains(rule, "://") {
		rule = "http://" + rule
	}
	u, err := url.Parse(rule)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if host == "" || strings.ContainsAny(host, " *") {
		return ""
	}
	return host
}

// Empty reports whether nothing is blocked.
func (b Blocklist) Empty() bool {
	return len(b.domains) == 0 && len(b.subdomains) == 0 && len(b.patterns) == 0
}

// Blocks reports whether a URL is ruled out.
func (b Blocklist) Blocks(rawURL string) bool {
	if b.Empty() {
		return false
	}
	for _, pattern := range b.patterns {
		if pattern.MatchString(rawURL) {
			return true
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range b.domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	for _, domain := range b.subdomains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// Domains are the plain-domain rules, which a search service can exclude
// itself — fewer of the results it returns are then thrown away here.
func (b Blocklist) Domains() []string {
	return append([]string(nil), b.domains...)
}
