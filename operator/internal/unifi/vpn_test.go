package unifi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const (
	testVpnServerID   = "e1000000-0000-4000-8000-000000000001"
	testVpnServerName = "Remote Access"
	testTunnelID      = "f1000000-0000-4000-8000-000000000002"
	testTunnelName    = "Branch Tunnel"
)

func TestListVpnServersPaginates(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	offsets := make([]string, 0, 2)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		switch offset {
		case "0":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"enabled":true,"id":"`+testVpnServerID+`","name":"`+testVpnServerName+`","type":"WIREGUARD","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"enabled":false,"id":"server-2","name":"Legacy","type":"L2TP","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	servers, err := client.ListVpnServers(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListVpnServers: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/vpn/servers", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want 2: %+v", len(servers), servers)
	}
	if servers[0].ID != testVpnServerID || servers[0].Type != "WIREGUARD" || !servers[0].Enabled {
		t.Errorf("unexpected first server: %+v", servers[0])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListSiteToSiteTunnelsPaginates(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	offsets := make([]string, 0, 2)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		switch offset {
		case "0":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"`+testTunnelID+`","name":"`+testTunnelName+`","type":"IPSEC","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"tunnel-2","name":"WG Tunnel","type":"WIREGUARD","metadata":{"origin":"DERIVED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	tunnels, err := client.ListSiteToSiteTunnels(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListSiteToSiteTunnels: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/vpn/site-to-site-tunnels", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(tunnels) != 2 {
		t.Fatalf("got %d tunnels, want 2: %+v", len(tunnels), tunnels)
	}
	if tunnels[0].ID != testTunnelID || tunnels[0].Type != "IPSEC" {
		t.Errorf("unexpected first tunnel: %+v", tunnels[0])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}
