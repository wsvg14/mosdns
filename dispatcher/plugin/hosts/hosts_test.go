//     Copyright (C) 2020-2021, IrineSistiana
//
//     This file is part of mosdns.
//
//     mosdns is free software: you can redistribute it and/or modify
//     it under the terms of the GNU General Public License as published by
//     the Free Software Foundation, either version 3 of the License, or
//     (at your option) any later version.
//
//     mosdns is distributed in the hope that it will be useful,
//     but WITHOUT ANY WARRANTY; without even the implied warranty of
//     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//     GNU General Public License for more details.
//
//     You should have received a copy of the GNU General Public License
//     along with this program.  If not, see <https://www.gnu.org/licenses/>.

package hosts

import (
	"bytes"
	"github.com/IrineSistiana/mosdns/dispatcher/handler"
	"github.com/IrineSistiana/mosdns/dispatcher/pkg/matcher/domain"
	"github.com/miekg/dns"
	"net"
	"testing"
)

var test_hosts = `
# comment
     # empty line
dns.google 8.8.8.8 8.8.4.4 2001:4860:4860::8844 2001:4860:4860::8888
regexp:^123456789 192.168.1.1
test.com 1.2.3.4
test.com 2.3.4.5
# nxdomain.com 1.2.3.4
`

func Test_hostsContainer_Match(t *testing.T) {
	m := domain.NewMixMatcher()
	m.SetPattenTypeMap(patternTypeMap)
	err := domain.LoadFromTextReader(m, bytes.NewBuffer([]byte(test_hosts)), parseIP)
	if err != nil {
		t.Fatal(err)
	}
	h := hostsContainer{
		BP:      handler.NewBP("test", PluginType),
		matcher: m,
	}

	type args struct {
		name string
		typ  uint16
	}
	tests := []struct {
		name        string
		args        args
		wantMatched bool
		wantAddr    []string
	}{
		{"matched A", args{name: "dns.google.", typ: dns.TypeA}, true, []string{"8.8.8.8", "8.8.4.4"}},
		{"matched AAAA", args{name: "dns.google.", typ: dns.TypeAAAA}, true, []string{"2001:4860:4860::8844", "2001:4860:4860::8888"}},
		{"not matched A", args{name: "nxdomain.com.", typ: dns.TypeA}, false, nil},
		{"not matched A", args{name: "sub.dns.google.", typ: dns.TypeA}, false, nil},
		{"matched regexp A", args{name: "123456789.test.", typ: dns.TypeA}, true, []string{"192.168.1.1"}},
		{"not matched regexp A", args{name: "0123456789.test.", typ: dns.TypeA}, false, nil},
		{"test appendable", args{name: "test.com.", typ: dns.TypeA}, true, []string{"1.2.3.4", "2.3.4.5"}},
	}
	for _, tt := range tests {
		q := new(dns.Msg)
		q.SetQuestion(tt.args.name, tt.args.typ)
		qCtx := handler.NewContext(q, nil)

		t.Run(tt.name, func(t *testing.T) {
			gotMatched := h.matchAndSet(qCtx)
			if gotMatched != tt.wantMatched {
				t.Fatalf("Match() gotMatched = %v, want %v", gotMatched, tt.wantMatched)
			}

			for _, s := range tt.wantAddr {
				wantIP := net.ParseIP(s)
				if wantIP == nil {
					t.Fatal("invalid test case addr")
				}
				found := false
				for _, rr := range qCtx.R().Answer {
					var ip net.IP
					switch rr := rr.(type) {
					case *dns.A:
						ip = rr.A
					case *dns.AAAA:
						ip = rr.AAAA
					default:
						continue
					}
					if ip.Equal(wantIP) {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("wanted ip is not found in response")
				}
			}
		})
	}
}
