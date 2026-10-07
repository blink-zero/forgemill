package ai

import (
	"regexp"
	"sort"
	"strings"
)

// Redaction runs on everything sent to a model. Placeholders are stable
// («REDACTED:kind») so the model can still reason about the structure of a
// script, and the answer is shown with the placeholders left in — nothing
// is ever un-redacted.

// RedactOptions tunes what counts as sensitive.
type RedactOptions struct {
	// Hostnames also redacts IPv4/IPv6 addresses and fully-qualified names.
	Hostnames bool
}

// RedactReport says how many of each kind were replaced.
type RedactReport struct {
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

// Kinds returns the redacted kinds, sorted, for display.
func (r RedactReport) Kinds() []string {
	kinds := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

func placeholder(kind string) string { return "«REDACTED:" + kind + "»" }

type rule struct {
	kind string
	re   *regexp.Regexp
	// repl builds the replacement from the match's submatches; nil = whole match.
	repl func(m []string) string
}

var (
	rePrivateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	reURLCreds   = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/\s:@]+):([^/\s@]+)@`)
	reAuthHeader = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*["']?\s*(?:bearer|basic|token)\s+)([A-Za-z0-9._~+/=-]+)`)
	reTokenShape = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,}|sk-ant-[A-Za-z0-9_-]{16,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{16,}|xox[baprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})\b`)
	reAssignment = regexp.MustCompile(`(?i)\b((?:[A-Z0-9_]*_)?(?:password|passwd|pwd|passphrase|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|client[_-]?secret)(?:[A-Z0-9_]*)?)(\s*[:=]\s*)(["']?)([^"'\s;&|<>` + "`" + `]+)`)
	reCLIFlag    = regexp.MustCompile(`(?i)(--?(?:password|passwd|pass|pw|token|api-key|apikey|secret|client-secret|access-key)(?:[= ]))(["']?)([^"'\s;&|<>]+)`)
	reIPv4       = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:/\d{1,2})?\b`)
	reIPv6       = regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b|\b(?:[0-9a-fA-F]{1,4}:){1,6}:(?:[0-9a-fA-F]{1,4}:){0,5}[0-9a-fA-F]{1,4}\b|\b(?:[0-9a-fA-F]{1,4}:){1,7}:(?:\s|$)|::(?:[0-9a-fA-F]{1,4}:){0,6}[0-9a-fA-F]{1,4}\b`)
	reFQDN       = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+(?:com|net|org|io|dev|cloud|local|internal|lan|home|corp|intra|net|biz|info|co|uk|de|fr|nl|au|nz|ca|us|eu|ch|at|se|no|dk|fi|pl|cz|es|it|jp|in|br)\b`)
)

var baseRules = []rule{
	{kind: "private-key", re: rePrivateKey},
	{kind: "credential", re: reURLCreds, repl: func(m []string) string { return m[1] + placeholder("credential") + "@" }},
	{kind: "token", re: reAuthHeader, repl: func(m []string) string { return m[1] + placeholder("token") }},
	{kind: "token", re: reTokenShape},
	{kind: "password", re: reAssignment, repl: func(m []string) string {
		if isPlaceholderOrRef(m[4]) {
			return m[0]
		}
		return m[1] + m[2] + m[3] + placeholder("password")
	}},
	{kind: "password", re: reCLIFlag, repl: func(m []string) string {
		if isPlaceholderOrRef(m[3]) {
			return m[0]
		}
		return m[1] + m[2] + placeholder("password")
	}},
}

var hostRules = []rule{
	{kind: "ip", re: reIPv4},
	{kind: "ip", re: reIPv6},
	{kind: "host", re: reFQDN},
}

// isPlaceholderOrRef leaves shell/parameter references alone: a script that
// says PASSWORD="$DB_PASSWORD" is doing the right thing and the model should
// see that it does.
func isPlaceholderOrRef(v string) bool {
	return strings.HasPrefix(v, "$") || strings.HasPrefix(v, "«") || strings.HasPrefix(v, "{{") || strings.HasPrefix(v, "<") || v == "" || v == "''" || v == `""`
}

// Redact replaces secrets (and, optionally, addresses) in s.
func Redact(s string, opts RedactOptions) (string, RedactReport) {
	report := RedactReport{Counts: map[string]int{}}
	rules := baseRules
	if opts.Hostnames {
		rules = append(append([]rule{}, baseRules...), hostRules...)
	}
	for _, r := range rules {
		s = r.re.ReplaceAllStringFunc(s, func(match string) string {
			if strings.Contains(match, "«REDACTED:") && r.repl == nil {
				return match
			}
			var out string
			if r.repl == nil {
				out = placeholder(r.kind)
			} else {
				out = r.repl(r.re.FindStringSubmatch(match))
			}
			if out != match {
				report.Counts[r.kind]++
				report.Total++
			}
			return out
		})
	}
	return s, report
}
