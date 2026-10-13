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

	var gotPath string
	offsets := make([]string, 0, 2)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		switch offset {
		case "0":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"`+testDnsPolicyID+`","type":"A_RECORD","domain":"`+testDnsDomain+`","enabled":true,
				 "ipv4Address":"192.168.1.10","ttlSeconds":14400,"metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"dns-2","type":"TXT_RECORD","domain":"text.example.com","enabled":false,"text":"hello",
				 "metadata":{"origin":"DERIVED"}}],
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
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2: %+v", len(policies), policies)
	}
	if policies[0].ID != testDnsPolicyID || policies[0].IPv4Address != "192.168.1.10" ||
		policies[0].TTLSeconds == nil || *policies[0].TTLSeconds != 14400 {
		t.Errorf("unexpected first policy: %+v", policies[0])
	}
	if policies[1].Type != "TXT_RECORD" || policies[1].Text != "hello" {
		t.Errorf("unexpected second policy: %+v", policies[1])
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListDnsPoliciesUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListDnsPolicies(context.Background(), testSiteID)
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
