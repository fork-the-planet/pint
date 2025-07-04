package checks_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudflare/pint/internal/checks"
	"github.com/cloudflare/pint/internal/promapi"
)

func TestRuleLinkCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Host") {
		case "200.example.com":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`OK`))
		case "400.example.com":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`Bad request`))
		case "404.example.com":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`Not found`))
		case "headers.example.com":
			if r.Header.Get("X-Auth") != "mykey" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`Access denied`))
			} else {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`OK`))
			}
		case "rewrite.example.com":
			if r.URL.Path != "/rewrite" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`Link not rewritten`))
			} else {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`OK`))
			}
		default:
			t.Logf("Invalid X-Host: %s", r.Header.Get("X-Host"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	testCases := []checkTest{
		{
			description: "ignores recording rules",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(checks.MustTemplatedRegexp(".*"), "", time.Second, nil, "", checks.Bug)
			},
			prometheus: noProm,
		},
		{
			description: "ignores unparsable link annotations",
			content:     "- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: \"%gh&%ij\"",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(checks.MustTemplatedRegexp(".*"), "", time.Second, nil, "", checks.Bug)
			},
			prometheus: noProm,
		},
		{
			description: "ignores non link annotations",
			content:     "- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: not a link",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(checks.MustTemplatedRegexp(".*"), "", time.Second, nil, "", checks.Bug)
			},
			prometheus: noProm,
		},
		{
			description: "ignores links without regexp match",
			content:     "- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: http://foo.example.com",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(checks.MustTemplatedRegexp("ftp://xxx.com"), "", time.Second, nil, "", checks.Bug)
			},
			prometheus: noProm,
		},
		{
			description: "link with no host",
			content:     "- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: http://",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(checks.MustTemplatedRegexp(".*"), "", time.Second, nil, "some text", checks.Bug)
			},
			prometheus: noProm,
			problems:   true,
		},
		{
			description: "link with 200 OK",
			content:     fmt.Sprintf("- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: %s/dashboard", srv.URL),
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp("http://.*"),
					"",
					time.Second,
					map[string]string{"X-Host": "200.example.com"},
					"",
					checks.Bug,
				)
			},
			prometheus: noProm,
		},
		{
			description: "link with 400 Bad Request",
			content:     fmt.Sprintf("- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: %s/dashboard", srv.URL),
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp("http://.*"),
					"",
					time.Second,
					map[string]string{"X-Host": "400.example.com"},
					"some text",
					checks.Bug,
				)
			},
			prometheus: noProm,
			problems:   true,
			snapshot: func(s string) string {
				return rewriteURL(s, srv.URL)
			},
		},
		{
			description: "multiple links, 400",
			content:     fmt.Sprintf("- alert: foo\n  expr: sum(foo)\n  annotations:\n    link1: %s/dashboard\n    link2: %s/graph", srv.URL, srv.URL),
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp("http://.*"),
					"",
					time.Second,
					map[string]string{"X-Host": "400.example.com"},
					"",
					checks.Warning,
				)
			},
			prometheus: noProm,
			problems:   true,
			snapshot: func(s string) string {
				return rewriteURL(s, srv.URL)
			},
		},
		{
			description: "link with headers set",
			content:     fmt.Sprintf("- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: %s/dashboard", srv.URL),
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp("http://.*"),
					"",
					time.Second,
					map[string]string{"X-Host": "headers.example.com", "X-Auth": "mykey"},
					"",
					checks.Bug,
				)
			},
			prometheus: noProm,
		},
		{
			description: "link with rewrite rule",
			content:     fmt.Sprintf("- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: %s/dashboard", srv.URL),
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp("http://(.*)"),
					srv.URL+"/rewrite",
					time.Second,
					map[string]string{"X-Host": "rewrite.example.com"},
					"",
					checks.Information,
				)
			},
			prometheus: noProm,
		},
		{
			description: "link with invalid URL",
			content:     "- alert: foo\n  expr: sum(foo)\n  annotations:\n    link: 'http://%41:8080/'",
			checker: func(_ *promapi.FailoverGroup) checks.RuleChecker {
				return checks.NewRuleLinkCheck(
					checks.MustTemplatedRegexp(".*"),
					"",
					time.Second,
					map[string]string{},
					"",
					checks.Bug,
				)
			},
			prometheus: noProm,
		},
	}
	runTests(t, testCases)
}
