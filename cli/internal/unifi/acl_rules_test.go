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
	testAclRuleID   = "4d5e6f70-8192-4a3b-c4d5-e6f708192a3b"
	testAclRuleName = "Allow Guest to DNS"
)

func TestListAclRulesPaginates(t *testing.T) {
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
				{"id":"`+testAclRuleID+`","index":0,"type":"IPV4","name":"`+testAclRuleName+`","enabled":true,
				 "action":"ALLOW",
				 "sourceFilter":{"type":"IP_ADDRESSES_OR_SUBNETS","ipAddressesOrSubnets":["192.168.20.0/24"],"portFilter":[53]},
				 "destinationFilter":{"type":"NETWORKS","networkIds":["`+testNetworkID+`"]},
				 "protocolFilter":["TCP","UDP"],
				 "enforcingDeviceFilter":{"type":"DEVICES","deviceIds":["`+testDeviceID+`"]},
				 "metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"rule-2","index":1,"type":"MAC","name":"Allow Switch MAC","enabled":true,"action":"ALLOW",
				 "sourceFilter":{"type":"MAC_ADDRESSES","macAddresses":["aa:bb:cc:dd:ee:ff"],"prefixLength":48},
				 "destinationFilter":{"type":"MAC_ADDRESSES","macAddresses":["00:11:22:33:44:55"]},
				 "networkIdFilter":"`+testNetworkID+`",
				 "metadata":{"origin":"USER_DEFINED"}}],
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
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %+v", len(rules), rules)
	}
	if rules[0].ID != testAclRuleID || rules[0].Type != "IPV4" || rules[0].Action != "ALLOW" {
		t.Errorf("unexpected first rule: %+v", rules[0])
	}
	if len(rules[0].Source.PortFilter) != 1 || rules[0].Source.PortFilter[0] != 53 {
		t.Errorf("unexpected first rule source filter: %+v", rules[0].Source)
	}
	if rules[0].EnforcingDeviceFilter == nil || len(rules[0].EnforcingDeviceFilter.DeviceIDs) != 1 {
		t.Errorf("unexpected first rule enforcing filter: %+v", rules[0].EnforcingDeviceFilter)
	}
	if rules[1].Type != "MAC" || rules[1].NetworkIDFilter != testNetworkID {
		t.Errorf("unexpected second rule: %+v", rules[1])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListAclRuleOrdering(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"orderedAclRuleIds":["`+testAclRuleID+`","rule-2"]}`)
	}))

	ids, err := client.ListAclRuleOrdering(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListAclRuleOrdering: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/acl-rules/ordering", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(ids) != 2 || ids[0] != testAclRuleID || ids[1] != "rule-2" {
		t.Errorf("orderedAclRuleIds = %v, want [%s rule-2]", ids, testAclRuleID)
	}
}

func TestListAclRulesUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListAclRules(context.Background(), testSiteID)
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
