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
	testPolicyID    = "7a6b5c4d-3e2f-4109-8a7b-6c5d4e3f2a10"
	testPolicyName  = "Allow LAN to WAN"
	testZoneUUID    = "2b1c0d9e-8f7a-4653-b2c1-d0e9f8a7b6c5"
	testListUUID    = "9e8d7c6b-5a4f-4321-9c8b-7a6d5e4f3c2b"
	testOtherZoneID = "3c2d1e0f-9a8b-4765-c3d2-e1f0a9b8c7d6"
)

// nestedMap returns the nested map at the given key path, failing the test when
// a path element is absent.
func nestedMap(t *testing.T, body map[string]any, path ...string) map[string]any {
	t.Helper()
	var current any = body
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %v is not an object", path, current)
		}
		current = m[key]
	}
	m, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("path %v: %v is not an object", path, current)
	}
	return m
}

func TestListFirewallPoliciesPaginates(t *testing.T) {
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
				{"id":"`+testPolicyID+`","index":0,"name":"`+testPolicyName+`","enabled":true,"loggingEnabled":false,
				 "action":{"type":"ALLOW","allowReturnTraffic":false},
				 "ipProtocolScope":{"ipVersion":"IPV4_AND_IPV6"},
				 "source":{"zoneId":"`+testZoneUUID+`"},"destination":{"zoneId":"`+testOtherZoneID+`"},
				 "metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"policy-2","index":1,"name":"Block","enabled":true,"loggingEnabled":false,
				 "action":{"type":"BLOCK"},
				 "ipProtocolScope":{"ipVersion":"IPV4"},
				 "source":{"zoneId":"`+testZoneUUID+`"},"destination":{"zoneId":"`+testOtherZoneID+`"},
				 "metadata":{"origin":"SYSTEM_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	policies, err := client.ListFirewallPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListFirewallPolicies: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2: %+v", len(policies), policies)
	}
	if policies[0].ID != testPolicyID || policies[0].Action.Type != testWifiActionAllow || policies[0].Source.ZoneID != testZoneUUID {
		t.Errorf("unexpected first policy: %+v", policies[0])
	}
	if policies[0].Metadata.Origin != testOriginUserDefined || policies[1].Metadata.Origin != testOriginSystem {
		t.Errorf("unexpected metadata: %+v", policies)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetFirewallPolicy(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"id":"`+testPolicyID+`","index":3,"name":"`+testPolicyName+`","enabled":true,"loggingEnabled":true,
			"action":{"type":"ALLOW","allowReturnTraffic":true},
			"ipProtocolScope":{"ipVersion":"IPV4"},
			"source":{"zoneId":"`+testZoneUUID+`","trafficFilter":{"type":"IP_ADDRESS","ipAddressFilter":{"type":"TRAFFIC_MATCHING_LIST","trafficMatchingListId":"`+testListUUID+`","matchOpposite":false}}},
			"destination":{"zoneId":"`+testOtherZoneID+`"},
			"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	policy, err := client.GetFirewallPolicy(context.Background(), testSiteID, testPolicyID)
	if err != nil {
		t.Fatalf("GetFirewallPolicy: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/%s", testSiteID, testPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if policy.ID != testPolicyID || policy.Index != 3 {
		t.Errorf("unexpected policy: %+v", policy)
	}
	if policy.Source.TrafficFilter == nil || policy.Source.TrafficFilter.IPAddressFilter == nil ||
		policy.Source.TrafficFilter.IPAddressFilter.TrafficMatchingListID != testListUUID {
		t.Errorf("source filter = %+v, want the traffic matching list UUID", policy.Source.TrafficFilter)
	}
}

func TestCreateFirewallPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["name"] != testPolicyName || body["enabled"] != true {
			t.Errorf("body name/enabled = %v/%v", body["name"], body["enabled"])
		}
		for _, key := range []string{"id", "index", testJSONFieldMetadata} {
			if _, ok := body[key]; ok {
				t.Errorf("request must not carry %q: %v", key, body)
			}
		}
		source := nestedMap(t, body, "source")
		if source["zoneId"] != testZoneUUID {
			t.Errorf("source.zoneId = %v, want %q", source["zoneId"], testZoneUUID)
		}
		ipFilter := nestedMap(t, body, "source", "trafficFilter", "ipAddressFilter")
		if ipFilter["type"] != "TRAFFIC_MATCHING_LIST" || ipFilter["trafficMatchingListId"] != testListUUID {
			t.Errorf("ipAddressFilter = %v, want the traffic matching list UUID", ipFilter)
		}
		action := nestedMap(t, body, "action")
		if action["type"] != testWifiActionAllow || action["allowReturnTraffic"] != true {
			t.Errorf("action = %v, want ALLOW with allowReturnTraffic", action)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testPolicyID+`","index":0,"name":"`+testPolicyName+`","metadata":{"origin":"USER_DEFINED"}}`)
	}))

	allow := true
	req := FirewallPolicyRequest{
		Name:           testPolicyName,
		Enabled:        true,
		LoggingEnabled: false,
		Action:         FirewallPolicyAction{Type: testWifiActionAllow, AllowReturnTraffic: &allow},
		IPProtocolScope: FirewallPolicyIPProtocolScope{
			IPVersion: "IPV4_AND_IPV6",
		},
		Source: FirewallPolicyZone{
			ZoneID: testZoneUUID,
			TrafficFilter: &FirewallPolicyTrafficFilter{
				Type: firewallTrafficFilterIPAddress,
				IPAddressFilter: &FirewallPolicyIPAddressFilter{
					Type:                  firewallFilterTrafficMatchingList,
					TrafficMatchingListID: testListUUID,
				},
			},
		},
		Destination: FirewallPolicyZone{ZoneID: testOtherZoneID},
	}
	policy, err := client.CreateFirewallPolicy(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateFirewallPolicy: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if policy.ID != testPolicyID {
		t.Errorf("policy ID = %q, want %q", policy.ID, testPolicyID)
	}
}

func TestUpdateFirewallPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"`+testPolicyID+`","index":0,"name":"`+testPolicyName+`","metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := FirewallPolicyRequest{
		Name:            testPolicyName,
		Enabled:         true,
		Action:          FirewallPolicyAction{Type: testActionBlock},
		IPProtocolScope: FirewallPolicyIPProtocolScope{IPVersion: testTypeIPv4},
		Source:          FirewallPolicyZone{ZoneID: testZoneUUID},
		Destination:     FirewallPolicyZone{ZoneID: testOtherZoneID},
	}
	if _, err := client.UpdateFirewallPolicy(context.Background(), testSiteID, testPolicyID, req); err != nil {
		t.Fatalf("UpdateFirewallPolicy: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/%s", testSiteID, testPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteFirewallPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteFirewallPolicy(context.Background(), testSiteID, testPolicyID); err != nil {
		t.Fatalf("DeleteFirewallPolicy: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/%s", testSiteID, testPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestGetFirewallPolicyNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"policy not found"}`)
	}))

	_, err := client.GetFirewallPolicy(context.Background(), testSiteID, testPolicyID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
}

func TestGetFirewallPolicyOrdering(t *testing.T) {
	t.Parallel()

	var gotPath, gotSource, gotDestination string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSource = r.URL.Query().Get("sourceFirewallZoneId")
		gotDestination = r.URL.Query().Get("destinationFirewallZoneId")
		_, _ = io.WriteString(w, `{"orderedFirewallPolicyIds":{"beforeSystemDefined":["`+testPolicyID+`"],"afterSystemDefined":[]}}`)
	}))

	ordering, err := client.GetFirewallPolicyOrdering(context.Background(), testSiteID, testZoneUUID, testOtherZoneID)
	if err != nil {
		t.Fatalf("GetFirewallPolicyOrdering: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotSource != testZoneUUID || gotDestination != testOtherZoneID {
		t.Errorf("zone query = %q/%q, want %q/%q", gotSource, gotDestination, testZoneUUID, testOtherZoneID)
	}
	if !reflect.DeepEqual(ordering.OrderedFirewallPolicyIDs.BeforeSystemDefined, []string{testPolicyID}) {
		t.Errorf("before = %v, want [%s]", ordering.OrderedFirewallPolicyIDs.BeforeSystemDefined, testPolicyID)
	}
}

func TestUpdateFirewallPolicyOrdering(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotSource, gotDestination string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotSource = r.URL.Query().Get("sourceFirewallZoneId")
		gotDestination = r.URL.Query().Get("destinationFirewallZoneId")
		body := decodeBody(t, r)
		ordered := nestedMap(t, body, "orderedFirewallPolicyIds")
		if before, ok := ordered["beforeSystemDefined"].([]any); !ok || len(before) != 1 || before[0] != testPolicyID {
			t.Errorf("beforeSystemDefined = %v, want [%s]", ordered["beforeSystemDefined"], testPolicyID)
		}
		_, _ = io.WriteString(w, `{"orderedFirewallPolicyIds":{"beforeSystemDefined":["`+testPolicyID+`"],"afterSystemDefined":[]}}`)
	}))

	req := FirewallPolicyOrdering{
		OrderedFirewallPolicyIDs: FirewallPolicyOrderingIDs{BeforeSystemDefined: []string{testPolicyID}},
	}
	if _, err := client.UpdateFirewallPolicyOrdering(context.Background(), testSiteID, testZoneUUID, testOtherZoneID, req); err != nil {
		t.Fatalf("UpdateFirewallPolicyOrdering: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotSource != testZoneUUID || gotDestination != testOtherZoneID {
		t.Errorf("zone query = %q/%q, want %q/%q", gotSource, gotDestination, testZoneUUID, testOtherZoneID)
	}
}
