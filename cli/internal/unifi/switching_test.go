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
	testLagID             = "1f2a3b4c-5d6e-4f70-8192-a3b4c5d6e7f8"
	testMcLagDomainID     = "d1e2f3a4-b5c6-4d7e-8f90-1a2b3c4d5e6f"
	testSwitchStackID     = "a4b5c6d7-e8f9-4091-a2b3-c4d5e6f70819"
	testSwitchMemberDevID = "aa1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

func TestListLagsPaginates(t *testing.T) {
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
				{"id":"`+testLagID+`","type":"MULTI_CHASSIS",
				 "members":[{"deviceId":"`+testSwitchMemberDevID+`","portIdxs":[1,2]}],
				 "mcLagDomainId":"`+testMcLagDomainID+`","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"lag-2","type":"LOCAL","members":[{"deviceId":"dev-2","portIdxs":[3]}],
				 "metadata":{"origin":"SYSTEM_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	lags, err := client.ListLags(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListLags: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/lags", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(lags) != 2 {
		t.Fatalf("got %d lags, want 2: %+v", len(lags), lags)
	}
	if lags[0].ID != testLagID || lags[0].Type != "MULTI_CHASSIS" || lags[0].McLagDomainID != testMcLagDomainID {
		t.Errorf("unexpected first lag: %+v", lags[0])
	}
	if len(lags[0].Members) != 1 || len(lags[0].Members[0].PortIdxs) != 2 {
		t.Errorf("unexpected first lag members: %+v", lags[0].Members)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListMcLagDomains(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testMcLagDomainID+`","name":"Core MC-LAG",
			 "peers":[{"deviceId":"`+testSwitchMemberDevID+`","linkPortIdxs":[7,8],"role":"TOP"}],
			 "lags":[{"id":"lag-1","members":[{"deviceId":"`+testSwitchMemberDevID+`","portIdxs":[9]}],"metadata":{"origin":"USER_DEFINED"}}],
			 "metadata":{"origin":"USER_DEFINED"}}],
			"limit":200,"offset":0,"totalCount":1}`)
	}))

	domains, err := client.ListMcLagDomains(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListMcLagDomains: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/mc-lag-domains", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(domains) != 1 || domains[0].Name != "Core MC-LAG" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	if len(domains[0].Peers) != 1 || domains[0].Peers[0].Role != "TOP" {
		t.Errorf("unexpected peers: %+v", domains[0].Peers)
	}
	if len(domains[0].Lags) != 1 || len(domains[0].Lags[0].Members) != 1 {
		t.Errorf("unexpected lags: %+v", domains[0].Lags)
	}
}

func TestListSwitchStacks(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testSwitchStackID+`","name":"Core Stack",
			 "members":[{"deviceId":"`+testSwitchMemberDevID+`"}],"lags":[],"metadata":{"origin":"SYSTEM_DEFINED"}}],
			"limit":200,"offset":0,"totalCount":1}`)
	}))

	stacks, err := client.ListSwitchStacks(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListSwitchStacks: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/switch-stacks", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(stacks) != 1 || stacks[0].Name != "Core Stack" {
		t.Fatalf("unexpected stacks: %+v", stacks)
	}
	if len(stacks[0].Members) != 1 || stacks[0].Members[0].DeviceID != testSwitchMemberDevID {
		t.Errorf("unexpected members: %+v", stacks[0].Members)
	}
}

func TestListLagsUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListLags(context.Background(), testSiteID)
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
