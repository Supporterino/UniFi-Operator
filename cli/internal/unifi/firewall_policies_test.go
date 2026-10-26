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
	testPolicyID      = "7a6b5c4d-3e2f-4109-8a7b-6c5d4e3f2a10"
	testPolicyName    = "Allow LAN to WAN"
	testPolicyZoneID  = "2b1c0d9e-8f7a-4653-b2c1-d0e9f8a7b6c5"
	testPolicyOtherID = "3c2d1e0f-9a8b-4765-c3d2-e1f0a9b8c7d6"
	testPolicyListID  = "9e8d7c6b-5a4f-4321-9c8b-7a6d5e4f3c2b"
)

func TestListFirewallPoliciesPaginates(t *testing.T) {
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
				{"id":"`+testPolicyID+`","index":0,"name":"`+testPolicyName+`","enabled":true,"loggingEnabled":false,
				 "action":{"type":"ALLOW","allowReturnTraffic":false},
				 "ipProtocolScope":{"ipVersion":"IPV4_AND_IPV6"},
				 "source":{"zoneId":"`+testPolicyZoneID+`",
				   "trafficFilter":{"type":"IP_ADDRESS","ipAddressFilter":{"type":"TRAFFIC_MATCHING_LIST","trafficMatchingListId":"`+testPolicyListID+`","matchOpposite":false}}},
				 "destination":{"zoneId":"`+testPolicyOtherID+`"},
				 "metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"policy-2","index":1,"name":"Block","enabled":true,"loggingEnabled":false,
				 "action":{"type":"BLOCK"},
				 "ipProtocolScope":{"ipVersion":"IPV4"},
				 "source":{"zoneId":"`+testPolicyZoneID+`"},"destination":{"zoneId":"`+testPolicyOtherID+`"},
				 "metadata":{"origin":"SYSTEM_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	policies, configured, err := client.ListFirewallPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListFirewallPolicies: %v", err)
	}
	if !configured {
		t.Error("zbfConfigured = false, want true for a populated policy page")
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2: %+v", len(policies), policies)
	}
	if policies[0].ID != testPolicyID || policies[0].Action.Type != "ALLOW" || policies[0].Source.ZoneID != testPolicyZoneID {
		t.Errorf("unexpected first policy: %+v", policies[0])
	}
	if policies[0].Source.TrafficFilter == nil || policies[0].Source.TrafficFilter.IPAddressFilter == nil ||
		policies[0].Source.TrafficFilter.IPAddressFilter.TrafficMatchingListID != testPolicyListID {
		t.Errorf("unexpected first policy filter: %+v", policies[0].Source.TrafficFilter)
	}
	if policies[0].Metadata.Origin != "USER_DEFINED" || policies[1].Metadata.Origin != "SYSTEM_DEFINED" {
		t.Errorf("unexpected metadata: %+v", policies)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetFirewallPolicyOrdering(t *testing.T) {
	t.Parallel()

	var gotPath, gotSource, gotDestination string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSource = r.URL.Query().Get("sourceFirewallZoneId")
		gotDestination = r.URL.Query().Get("destinationFirewallZoneId")
		_, _ = io.WriteString(w, `{"orderedFirewallPolicyIds":{
			"beforeSystemDefined":["`+testPolicyID+`"],
			"afterSystemDefined":["`+testPolicyOtherID+`"]}}`)
	}))

	ordering, err := client.GetFirewallPolicyOrdering(context.Background(), testSiteID, testPolicyZoneID, testPolicyOtherID)
	if err != nil {
		t.Fatalf("GetFirewallPolicyOrdering: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/policies/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotSource != testPolicyZoneID {
		t.Errorf("sourceFirewallZoneId = %q, want %q", gotSource, testPolicyZoneID)
	}
	if gotDestination != testPolicyOtherID {
		t.Errorf("destinationFirewallZoneId = %q, want %q", gotDestination, testPolicyOtherID)
	}
	before := ordering.OrderedFirewallPolicyIDs.BeforeSystemDefined
	after := ordering.OrderedFirewallPolicyIDs.AfterSystemDefined
	if len(before) != 1 || before[0] != testPolicyID {
		t.Errorf("beforeSystemDefined = %v, want [%s]", before, testPolicyID)
	}
	if len(after) != 1 || after[0] != testPolicyOtherID {
		t.Errorf("afterSystemDefined = %v, want [%s]", after, testPolicyOtherID)
	}
}

// TestListFirewallPoliciesUnconfiguredIsEmpty pins the family-wide mapping (design D2): a
// console with Zone-Based Firewall off answers the policies read with the same
// unconfigured-subsystem code the zones read returns, which must resolve to an empty set
// rather than an error so `snapshot firewall-policies` succeeds with zero resources. The flag
// records that the read did so, so a caller can explain the gap instead of degrading silently.
func TestListFirewallPoliciesUnconfiguredIsEmpty(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"statusCode":400,"statusName":"BAD_REQUEST",`+
			`"code":"api.firewall.zone-based-firewall-not-configured",`+
			`"message":"Zone-based firewall is not configured"}`)
	}))

	policies, configured, err := client.ListFirewallPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListFirewallPolicies: %v, want nil error for an unconfigured subsystem", err)
	}
	if configured {
		t.Error("zbfConfigured = true, want false for the unconfigured-subsystem code")
	}
	if len(policies) != 0 {
		t.Errorf("policies = %+v, want an empty set", policies)
	}
}

// TestListFirewallPoliciesEmptyPageIsConfigured pins the flag's contract the same way the zones
// read does (design D1): an empty 200 page is not evidence that Zone-Based Firewall is off, so
// zbfConfigured must stay true.
func TestListFirewallPoliciesEmptyPageIsConfigured(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`)
	}))

	policies, configured, err := client.ListFirewallPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListFirewallPolicies: %v", err)
	}
	if !configured {
		t.Error("zbfConfigured = false, want true for an empty 200 page")
	}
	if len(policies) != 0 {
		t.Errorf("policies = %+v, want an empty set", policies)
	}
}

// TestListFirewallPoliciesOtherErrorSurfaces guards against widening the mapping: only the
// unconfigured-subsystem code is tolerated, so another 400 still fails the read and leaves the
// flag true.
func TestListFirewallPoliciesOtherErrorSurfaces(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"statusCode":400,"statusName":"BAD_REQUEST",`+
			`"code":"api.firewall.policy.invalid","message":"invalid policy query"}`)
	}))

	policies, configured, err := client.ListFirewallPolicies(context.Background(), testSiteID)
	if err == nil {
		t.Fatal("ListFirewallPolicies returned no error for an unrelated 400")
	}
	if !configured {
		t.Error("zbfConfigured = false, want true when the code is not the unconfigured subsystem")
	}
	if len(policies) != 0 {
		t.Errorf("policies = %+v, want none", policies)
	}
}

// TestGetFirewallPolicyOrderingUnconfiguredSurfaces pins design D2's deliberate omission: the
// ordering read stays unmapped, so a ZBF-unconfigured response is still an error rather than a
// silently empty ordering.
func TestGetFirewallPolicyOrderingUnconfiguredSurfaces(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"statusCode":400,"statusName":"BAD_REQUEST",`+
			`"code":"api.firewall.zone-based-firewall-not-configured",`+
			`"message":"Zone-based firewall is not configured"}`)
	}))

	_, err := client.GetFirewallPolicyOrdering(context.Background(), testSiteID, testPolicyZoneID, testPolicyOtherID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.Code != "api.firewall.zone-based-firewall-not-configured" {
		t.Errorf("Code = %q, want the unconfigured-subsystem code", apiErr.Code)
	}
}

func TestListFirewallPoliciesUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, configured, err := client.ListFirewallPolicies(context.Background(), testSiteID)
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
	if !configured {
		t.Error("zbfConfigured = false, want true for an authentication failure")
	}
}
