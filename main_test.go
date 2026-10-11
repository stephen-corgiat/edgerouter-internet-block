package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

type fakeIPSet struct {
	mu      sync.Mutex
	members map[string]struct{}
	addErr  error
	delErr  error
	listErr error
}

func newFakeIPSet(initial ...string) *fakeIPSet {
	f := &fakeIPSet{members: map[string]struct{}{}}
	for _, ip := range initial {
		f.members[ip] = struct{}{}
	}
	return f
}

func (f *fakeIPSet) Add(ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	f.members[ip] = struct{}{}
	return nil
}

func (f *fakeIPSet) Del(ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.members, ip)
	return nil
}

func (f *fakeIPSet) Members() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]string, 0, len(f.members))
	for ip := range f.members {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out, nil
}

func testConfig() *Config {
	return &Config{
		ListenAddr: "127.0.0.1",
		Port:       8080,
		AccessKey:  "secret",
		IPSetNames: map[string]string{
			"internet": "blocked-internet-dynamic",
			"games":    "blocked-games-dynamic",
		},
	}
}

type testServer struct {
	mux    http.Handler
	ipsets map[string]*fakeIPSet
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	cfg := testConfig()
	ipsets := map[string]*fakeIPSet{}
	asIface := map[string]IPSet{}
	for typ := range cfg.IPSetNames {
		f := newFakeIPSet()
		ipsets[typ] = f
		asIface[typ] = f
	}
	return &testServer{
		mux:    NewServer(cfg, asIface).Routes(),
		ipsets: ipsets,
	}
}

func (ts *testServer) seed(typ string, ips ...string) {
	for _, ip := range ips {
		ts.ipsets[typ].members[ip] = struct{}{}
	}
}

func authedRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer secret")
	return req
}

func TestAddPutsRequestedIPs(t *testing.T) {
	ts := newTestServer(t)

	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/internet/add?ip=192.168.1.10,192.168.1.11"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp toggleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Type != "internet" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if got, want := resp.Members, []string{"192.168.1.10", "192.168.1.11"}; !equal(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
}

func TestAddIsolatesTypes(t *testing.T) {
	ts := newTestServer(t)

	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/games/add?ip=192.168.1.20"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}

	if m, _ := ts.ipsets["games"].Members(); !equal(m, []string{"192.168.1.20"}) {
		t.Errorf("games members = %v", m)
	}
	if m, _ := ts.ipsets["internet"].Members(); len(m) != 0 {
		t.Errorf("internet should be untouched, got %v", m)
	}
}

func TestDelRemovesRequestedOnly(t *testing.T) {
	ts := newTestServer(t)
	ts.seed("internet", "192.168.1.10", "192.168.1.11", "192.168.1.20", "192.168.1.99")

	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/internet/del?ip=192.168.1.10,192.168.1.11"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	members, _ := ts.ipsets["internet"].Members()
	if !equal(members, []string{"192.168.1.20", "192.168.1.99"}) {
		t.Errorf("members = %v", members)
	}
}

func TestStatusReportsPerDevice(t *testing.T) {
	ts := newTestServer(t)
	ts.seed("internet", "192.168.1.10")

	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodGet, "/internet/status?ip=192.168.1.10,192.168.1.11"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AllPresent {
		t.Errorf("all_present should be false")
	}
	want := map[string]bool{"192.168.1.10": true, "192.168.1.11": false}
	for _, d := range resp.Devices {
		if want[d.IP] != d.Present {
			t.Errorf("device %s present=%v, want %v", d.IP, d.Present, want[d.IP])
		}
	}
}

func TestStatusAllPresentWhenAllInSet(t *testing.T) {
	ts := newTestServer(t)
	ts.seed("internet", "192.168.1.10", "192.168.1.11")

	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodGet, "/internet/status?ip=192.168.1.10,192.168.1.11"))

	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.AllPresent {
		t.Errorf("expected all_present=true, got %+v", resp)
	}
}

func TestUnknownTypeReturns404(t *testing.T) {
	ts := newTestServer(t)
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/social/add?ip=192.168.1.10"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rr.Code, rr.Body.String())
	}
}

func TestMissingIPReturns400(t *testing.T) {
	ts := newTestServer(t)
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/internet/add"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
}

func TestInvalidIPReturns400(t *testing.T) {
	ts := newTestServer(t)
	cases := []string{
		"/internet/add?ip=not-an-ip",
		"/internet/add?ip=192.168.1.10,bogus",
		"/internet/add?ip=192.168.1.10,",
		"/internet/add?ip=--exist",
		"/internet/add?ip=192.168.1.10%20--exist",
	}
	for _, path := range cases {
		rr := httptest.NewRecorder()
		ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, path))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400, body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestAuthRejectsMissingHeader(t *testing.T) {
	ts := newTestServer(t)
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/internet/status?ip=192.168.1.10", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestAuthRejectsWrongKey(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/internet/status?ip=192.168.1.10", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestAddIsIdempotent(t *testing.T) {
	ts := newTestServer(t)
	ts.seed("internet", "192.168.1.10", "192.168.1.11")
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, authedRequest(http.MethodPost, "/internet/add?ip=192.168.1.10,192.168.1.11"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	members, _ := ts.ipsets["internet"].Members()
	if !equal(members, []string{"192.168.1.10", "192.168.1.11"}) {
		t.Errorf("members = %v", members)
	}
}

func TestParseMembers(t *testing.T) {
	out := `Name: blocked-internet-dynamic
Type: hash:net
Header: family inet hashsize 1024 maxelem 65536
Size in memory: 736
References: 1
Number of entries: 2
Members:
192.168.1.10
192.168.1.11
`
	got := parseMembers([]byte(out))
	want := []string{"192.168.1.10", "192.168.1.11"}
	if !equal(got, want) {
		t.Errorf("parseMembers = %v, want %v", got, want)
	}
}

func TestConfigValidate(t *testing.T) {
	good := func() Config {
		return Config{
			AccessKey:  "k",
			IPSetNames: map[string]string{"internet": "blocked-internet-dynamic"},
		}
	}
	mutate := func(f func(*Config)) Config {
		c := good()
		f(&c)
		return c
	}
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"missing key", mutate(func(c *Config) { c.AccessKey = "" }), "access_key"},
		{"no ipset_names", mutate(func(c *Config) { c.IPSetNames = nil }), "ipset_names"},
		{"empty ipset name", mutate(func(c *Config) { c.IPSetNames["internet"] = "" }), "ipset_names"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.applyDefaults()
			err := tc.cfg.validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
