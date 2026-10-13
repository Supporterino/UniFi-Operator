package unifi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
)

const (
	testLagID           = "aa000000-0000-4000-8000-000000000001"
	testLagName         = "Core LAG"
	testMcLagDomainID   = "bb000000-0000-4000-8000-000000000002"
	testMcLagDomainName = "Core MC-LAG"
	testSwitchStackID   = "cc000000-0000-4000-8000-000000000003"
	testSwitchStackName = "Core Stack"
	testSwitchDeviceID  = "dd000000-0000-4000-8000-000000000004"
)

func TestListLagsPaginates(t *testing.T) {
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
				{"id":"`+testLagID+`","name":"`+testLagName+`","type":"LOCAL","members":[{"deviceId":"`+testSwitchDeviceID+`","portIdxs":[1,2]}],"metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"lag-2","type":"MULTI_CHASSIS","members":[],"mcLagDomainId":"`+testMcLagDomainID+`","metadata":{"origin":"USER_DEFINED"}}],
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
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(lags) != 2 {
		t.Fatalf("got %d lags, want 2: %+v", len(lags), lags)
	}
	if lags[0].ID != testLagID || len(lags[0].Members) != 1 || !reflect.DeepEqual(lags[0].Members[0].PortIdxs, []int32{1, 2}) {
		t.Errorf("unexpected first lag: %+v", lags[0])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetLag(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"id":"`+testLagID+`","type":"LOCAL","members":[{"deviceId":"`+testSwitchDeviceID+`","portIdxs":[3]}],"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	lag, err := client.GetLag(context.Background(), testSiteID, testLagID)
	if err != nil {
		t.Fatalf("GetLag: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/lags/%s", testSiteID, testLagID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if lag.Type != "LOCAL" || len(lag.Members) != 1 || !reflect.DeepEqual(lag.Members[0].PortIdxs, []int32{3}) {
		t.Errorf("unexpected lag: %+v", lag)
	}
}

func TestGetLagNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"lag not found"}`)
	}))

	_, err := client.GetLag(context.Background(), testSiteID, testLagID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
}

func TestListMcLagDomains(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testMcLagDomainID+`","name":"`+testMcLagDomainName+`","metadata":{"origin":"USER_DEFINED"},
			 "peers":[{"deviceId":"`+testSwitchDeviceID+`","linkPortIdxs":[1],"role":"TOP"}],
			 "lags":[{"id":"lag-1","metadata":{"origin":"USER_DEFINED"},"members":[{"deviceId":"`+testSwitchDeviceID+`","portIdxs":[1,2]}]}]}],
			"limit":25,"offset":0,"totalCount":1}`)
	}))

	domains, err := client.ListMcLagDomains(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListMcLagDomains: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/mc-lag-domains", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(domains) != 1 || domains[0].Name != testMcLagDomainName || len(domains[0].Peers) != 1 {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	if domains[0].Peers[0].Role != "TOP" || len(domains[0].Lags) != 1 {
		t.Errorf("unexpected domain detail: %+v", domains[0])
	}
}

func TestGetMcLagDomain(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"`+testMcLagDomainID+`","name":"`+testMcLagDomainName+`","metadata":{"origin":"USER_DEFINED"},"peers":[],"lags":[]}`)
	}))

	domain, err := client.GetMcLagDomain(context.Background(), testSiteID, testMcLagDomainID)
	if err != nil {
		t.Fatalf("GetMcLagDomain: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/mc-lag-domains/%s", testSiteID, testMcLagDomainID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if domain.Name != testMcLagDomainName {
		t.Errorf("domain name = %q, want %q", domain.Name, testMcLagDomainName)
	}
}

func TestListSwitchStacks(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testSwitchStackID+`","name":"`+testSwitchStackName+`","metadata":{"origin":"USER_DEFINED"},
			 "members":[{"deviceId":"`+testSwitchDeviceID+`"},{"deviceId":"ee000000-0000-4000-8000-000000000005"}],
			 "lags":[{"id":"lag-1","metadata":{"origin":"USER_DEFINED"},"members":[{"deviceId":"`+testSwitchDeviceID+`","portIdxs":[1]}]}]}],
			"limit":25,"offset":0,"totalCount":1}`)
	}))

	stacks, err := client.ListSwitchStacks(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListSwitchStacks: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/switch-stacks", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(stacks) != 1 || stacks[0].Name != testSwitchStackName || len(stacks[0].Members) != 2 {
		t.Fatalf("unexpected stacks: %+v", stacks)
	}
}

func TestGetSwitchStack(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"`+testSwitchStackID+`","name":"`+testSwitchStackName+`","metadata":{"origin":"USER_DEFINED"},"members":[{"deviceId":"`+testSwitchDeviceID+`"}],"lags":[]}`)
	}))

	stack, err := client.GetSwitchStack(context.Background(), testSiteID, testSwitchStackID)
	if err != nil {
		t.Fatalf("GetSwitchStack: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/switching/switch-stacks/%s", testSiteID, testSwitchStackID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if stack.ID != testSwitchStackID || len(stack.Members) != 1 {
		t.Errorf("unexpected stack: %+v", stack)
	}
}
