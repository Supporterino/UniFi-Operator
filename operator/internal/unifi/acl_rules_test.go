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
	testAclRuleID   = "b1a2c3d4-e5f6-4708-9a1b-2c3d4e5f6071"
	testAclRuleName = "Block guest"
	testAclDeviceID = "aa11bb22-cc33-4d44-8e55-ff66aa77bb88"
)

func TestListAclRulesPaginates(t *testing.T) {
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
				{"id":"`+testAclRuleID+`","index":0,"type":"IPV4","name":"`+testAclRuleName+`","enabled":true,"action":"BLOCK",
				 "sourceFilter":{"type":"NETWORKS","networkIds":["`+testNetworkID+`"]},
				 "destinationFilter":{"type":"PORTS","portFilter":[80,443]},
				 "metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"rule-2","index":1,"type":"MAC","name":"derived","enabled":true,"action":"ALLOW",
				 "sourceFilter":{"type":"MAC_ADDRESSES","macAddresses":["aa:bb:cc:dd:ee:ff"]},
				 "destinationFilter":{"type":"MAC_ADDRESSES","macAddresses":["aa:bb:cc:dd:ee:ff"]},
				 "metadata":{"origin":"DERIVED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	rules, err := client.ListAclRules(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListAclRules: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %+v", len(rules), rules)
	}
	if rules[0].ID != testAclRuleID || rules[0].Source.NetworkIDs[0] != testNetworkID {
		t.Errorf("unexpected first rule: %+v", rules[0])
	}
	if !reflect.DeepEqual(rules[0].Destination.PortFilter, []int32{80, 443}) {
		t.Errorf("destination ports = %v, want [80 443]", rules[0].Destination.PortFilter)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetAclRule(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"id":"`+testAclRuleID+`","index":2,"type":"MAC","name":"`+testAclRuleName+`","enabled":true,"action":"ALLOW",
			"networkIdFilter":"`+testNetworkID+`",
			"sourceFilter":{"type":"MAC_ADDRESSES","macAddresses":["aa:bb:cc:dd:ee:ff"]},
			"destinationFilter":{"type":"MAC_ADDRESSES","macAddresses":["11:22:33:44:55:66"],"prefixLength":24},
			"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	rule, err := client.GetAclRule(context.Background(), testSiteID, testAclRuleID)
	if err != nil {
		t.Fatalf("GetAclRule: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/%s", testSiteID, testAclRuleID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if rule.ID != testAclRuleID || rule.NetworkIDFilter != testNetworkID {
		t.Errorf("unexpected rule: %+v", rule)
	}
	if rule.Destination.PrefixLength == nil || *rule.Destination.PrefixLength != 24 {
		t.Errorf("destination prefixLength = %v, want 24", rule.Destination.PrefixLength)
	}
}

func TestCreateAclRule(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["type"] != testTypeIPv4 || body["action"] != testActionBlock {
			t.Errorf("body type/action = %v/%v", body["type"], body["action"])
		}
		for _, key := range []string{"id", "index", testJSONFieldMetadata} {
			if _, ok := body[key]; ok {
				t.Errorf("request must not carry %q: %v", key, body)
			}
		}
		source := nestedMap(t, body, "sourceFilter")
		if source["type"] != "NETWORKS" {
			t.Errorf("sourceFilter.type = %v, want NETWORKS", source["type"])
		}
		dest := nestedMap(t, body, "destinationFilter")
		if dest["type"] != testEndpointPorts {
			t.Errorf("destinationFilter.type = %v, want PORTS", dest["type"])
		}
		device := nestedMap(t, body, "enforcingDeviceFilter")
		if device["type"] != "DEVICES" {
			t.Errorf("enforcingDeviceFilter.type = %v, want DEVICES", device["type"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testAclRuleID+`","index":0,"type":"IPV4","name":"`+testAclRuleName+`","enabled":true,"action":"BLOCK","metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := AclRuleRequest{
		Type:                  testTypeIPv4,
		Name:                  testAclRuleName,
		Enabled:               true,
		Action:                testActionBlock,
		Source:                AclRuleEndpoint{Type: "NETWORKS", NetworkIDs: []string{testNetworkID}},
		Destination:           AclRuleEndpoint{Type: testEndpointPorts, PortFilter: []int32{80, 443}},
		ProtocolFilter:        []string{"TCP"},
		EnforcingDeviceFilter: &AclRuleDeviceFilter{Type: "DEVICES", DeviceIDs: []string{testAclDeviceID}},
	}
	rule, err := client.CreateAclRule(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateAclRule: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if rule.ID != testAclRuleID {
		t.Errorf("rule ID = %q, want %q", rule.ID, testAclRuleID)
	}
}

func TestUpdateAclRule(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["type"] != "MAC" || body["networkIdFilter"] != testNetworkID {
			t.Errorf("body type/networkIdFilter = %v/%v", body["type"], body["networkIdFilter"])
		}
		_, _ = io.WriteString(w, `{"id":"`+testAclRuleID+`","index":0,"type":"MAC","name":"`+testAclRuleName+`","metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := AclRuleRequest{
		Type:            "MAC",
		Name:            testAclRuleName,
		Enabled:         true,
		Action:          testWifiActionAllow,
		Source:          AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
		Destination:     AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"11:22:33:44:55:66"}},
		NetworkIDFilter: testNetworkID,
	}
	if _, err := client.UpdateAclRule(context.Background(), testSiteID, testAclRuleID, req); err != nil {
		t.Fatalf("UpdateAclRule: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/%s", testSiteID, testAclRuleID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteAclRule(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteAclRule(context.Background(), testSiteID, testAclRuleID); err != nil {
		t.Fatalf("DeleteAclRule: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/%s", testSiteID, testAclRuleID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestGetAclRuleNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"rule not found"}`)
	}))

	_, err := client.GetAclRule(context.Background(), testSiteID, testAclRuleID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
}

func TestGetAclRuleOrdering(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"orderedAclRuleIds":["`+testAclRuleID+`"]}`)
	}))

	ordering, err := client.GetAclRuleOrdering(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("GetAclRuleOrdering: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !reflect.DeepEqual(ordering.OrderedAclRuleIDs, []string{testAclRuleID}) {
		t.Errorf("orderedAclRuleIds = %v, want [%s]", ordering.OrderedAclRuleIDs, testAclRuleID)
	}
}

func TestUpdateAclRuleOrdering(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		ids, ok := body["orderedAclRuleIds"].([]any)
		if !ok || len(ids) != 1 || ids[0] != testAclRuleID {
			t.Errorf("orderedAclRuleIds = %v, want [%s]", body["orderedAclRuleIds"], testAclRuleID)
		}
		_, _ = io.WriteString(w, `{"orderedAclRuleIds":["`+testAclRuleID+`"]}`)
	}))

	req := AclRuleOrdering{OrderedAclRuleIDs: []string{testAclRuleID}}
	if _, err := client.UpdateAclRuleOrdering(context.Background(), testSiteID, req); err != nil {
		t.Fatalf("UpdateAclRuleOrdering: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}
