package unifi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const (
	testVpnServerID   = "a1b2c3d4-e5f6-4071-8293-a4b5c6d7e8f9"
	testVpnServerName = "WireGuard Server"
	testTunnelID      = "d4e5f607-1829-43a4-b5c6-d7e8f9012345"
	testTunnelName    = "HQ to Branch"
)

func TestListVpnServersPaginates(t *testing.T) {
	t.Parallel()

	var gotPath string
	offsets := make([]string, 0, 2)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
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
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want 2: %+v", len(servers), servers)
	}
	if servers[0].ID != testVpnServerID || servers[0].Type != "WIREGUARD" || !servers[0].Enabled {
		t.Errorf("unexpected first server: %+v", servers[0])
	}
	if servers[1].Enabled {
		t.Errorf("unexpected second server enabled: %+v", servers[1])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListSiteToSiteTunnelsPaginates(t *testing.T) {
	t.Parallel()

	var gotPath string
	offsets := make([]string, 0, 2)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
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

func TestListVpnServersUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListVpnServers(context.Background(), testSiteID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Message != "Missing credentials" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "Missing credentials")
	}
}
