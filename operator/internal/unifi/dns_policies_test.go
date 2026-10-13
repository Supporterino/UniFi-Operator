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
	testDnsPolicyID = "c2b3a4d5-e6f7-4809-a1b2-c3d4e5f60718"
	testDnsDomain   = "example.com"
)

func TestListDnsPoliciesPaginates(t *testing.T) {
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
				{"id":"`+testDnsPolicyID+`","type":"A_RECORD","domain":"`+testDnsDomain+`","enabled":true,"ipv4Address":"192.168.1.10","ttlSeconds":14400,"metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"dns-2","type":"TXT_RECORD","domain":"text.example.com","enabled":false,"text":"hello","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	policies, err := client.ListDnsPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListDnsPolicies: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/dns/policies", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2: %+v", len(policies), policies)
	}
	if policies[0].ID != testDnsPolicyID || policies[0].IPv4Address != testIPv4Address || policies[0].TTLSeconds == nil {
		t.Errorf("unexpected first policy: %+v", policies[0])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetDnsPolicy(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"id":"`+testDnsPolicyID+`","type":"MX_RECORD","domain":"`+testDnsDomain+`","enabled":true,"mailServerDomain":"mail.example.com","priority":10,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	policy, err := client.GetDnsPolicy(context.Background(), testSiteID, testDnsPolicyID)
	if err != nil {
		t.Fatalf("GetDnsPolicy: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/dns/policies/%s", testSiteID, testDnsPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if policy.MailServerDomain != "mail.example.com" || policy.Priority == nil || *policy.Priority != 10 {
		t.Errorf("unexpected policy: %+v", policy)
	}
}

func TestCreateDnsPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["type"] != testDnsTypeARecord || body["domain"] != testDnsDomain || body["enabled"] != true {
			t.Errorf("body = %v", body)
		}
		if body["ipv4Address"] != testIPv4Address || body["ttlSeconds"] != float64(14400) {
			t.Errorf("A-record fields = %v", body)
		}
		for _, key := range []string{"id", testJSONFieldMetadata} {
			if _, ok := body[key]; ok {
				t.Errorf("request must not carry %q: %v", key, body)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testDnsPolicyID+`","type":"A_RECORD","domain":"`+testDnsDomain+`","enabled":true,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	ttl := int32(14400)
	req := DnsPolicyRequest{Type: testDnsTypeARecord, Domain: testDnsDomain, Enabled: true, IPv4Address: testIPv4Address, TTLSeconds: &ttl}
	policy, err := client.CreateDnsPolicy(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateDnsPolicy: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/dns/policies", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if policy.ID != testDnsPolicyID {
		t.Errorf("policy ID = %q, want %q", policy.ID, testDnsPolicyID)
	}
}

// TestCreateDnsPolicySerializesZeroTTL pins that a legitimate zero TTL is sent
// rather than omitted.
func TestCreateDnsPolicySerializesZeroTTL(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if ttl, ok := body["ttlSeconds"]; !ok || ttl != float64(0) {
			t.Errorf("ttlSeconds = %v, want 0 present", body["ttlSeconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testDnsPolicyID+`","type":"A_RECORD","domain":"`+testDnsDomain+`","enabled":true,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	ttl := int32(0)
	req := DnsPolicyRequest{Type: testDnsTypeARecord, Domain: testDnsDomain, Enabled: true, IPv4Address: testIPv4Address, TTLSeconds: &ttl}
	if _, err := client.CreateDnsPolicy(context.Background(), testSiteID, req); err != nil {
		t.Fatalf("CreateDnsPolicy: %v", err)
	}
}

func TestUpdateDnsPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["type"] != "SRV_RECORD" || body["port"] != float64(389) || body["service"] != "_ldap" {
			t.Errorf("SRV fields = %v", body)
		}
		_, _ = io.WriteString(w, `{"id":"`+testDnsPolicyID+`","type":"SRV_RECORD","domain":"`+testDnsDomain+`","enabled":true,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	port, priority, weight := int32(389), int32(10), int32(5)
	req := DnsPolicyRequest{
		Type: "SRV_RECORD", Domain: testDnsDomain, Enabled: true,
		Port: &port, Priority: &priority, Protocol: "_tcp", ServerDomain: "server.example.com", Service: "_ldap", Weight: &weight,
	}
	if _, err := client.UpdateDnsPolicy(context.Background(), testSiteID, testDnsPolicyID, req); err != nil {
		t.Fatalf("UpdateDnsPolicy: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/dns/policies/%s", testSiteID, testDnsPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteDnsPolicy(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteDnsPolicy(context.Background(), testSiteID, testDnsPolicyID); err != nil {
		t.Fatalf("DeleteDnsPolicy: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/dns/policies/%s", testSiteID, testDnsPolicyID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestGetDnsPolicyNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"record not found"}`)
	}))

	_, err := client.GetDnsPolicy(context.Background(), testSiteID, testDnsPolicyID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
}
