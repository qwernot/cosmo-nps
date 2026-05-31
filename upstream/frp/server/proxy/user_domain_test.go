package proxy

import "testing"

func TestDomainAllowed(t *testing.T) {
	pools := []string{"*.example.com", "app.test.com"}
	cases := []struct {
		name   string
		domain string
		want   bool
	}{
		{name: "wildcard child", domain: "web.example.com", want: true},
		{name: "wildcard does not match apex", domain: "example.com", want: false},
		{name: "exact", domain: "app.test.com", want: true},
		{name: "outside", domain: "bad.example.net", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domainAllowed(tc.domain, pools); got != tc.want {
				t.Fatalf("domainAllowed(%q) = %v, want %v", tc.domain, got, tc.want)
			}
		})
	}
}
