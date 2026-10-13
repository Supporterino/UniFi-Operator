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
	testTrafficMatchingListID   = "7c1e2f3a-4b5c-4d6e-8f90-1a2b3c4d5e6f"
	testTrafficMatchingListName = "Corp Addresses"
)

func TestListTrafficMatchingListsPaginates(t *testing.T) {
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
				{"id":"`+testTrafficMatchingListID+`","name":"`+testTrafficMatchingListName+`","type":"IPV4_ADDRESSES",
				 "items":[{"type":"IP_ADDRESS","value":"10.0.0.1"},{"type":"SUBNET","value":"10.0.0.0/24"}]}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"list-2","name":"Web Ports","type":"PORTS",
				 "items":[{"type":"PORT_NUMBER","value":"443"},{"type":"PORT_NUMBER_RANGE","start":"8000","stop":"8080"}]}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	lists, err := client.ListTrafficMatchingLists(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListTrafficMatchingLists: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/traffic-matching-lists", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(lists) != 2 {
		t.Fatalf("got %d lists, want 2: %+v", len(lists), lists)
	}
	if lists[0].ID != testTrafficMatchingListID || lists[0].Name != testTrafficMatchingListName || lists[0].Type != "IPV4_ADDRESSES" {
		t.Errorf("unexpected first list: %+v", lists[0])
	}
	if len(lists[0].Items) != 2 || lists[0].Items[0].Value != "10.0.0.1" || lists[0].Items[1].Value != "10.0.0.0/24" {
		t.Errorf("unexpected first list items: %+v", lists[0].Items)
	}
	if len(lists[1].Items) != 2 || lists[1].Items[1].Start != "8000" || lists[1].Items[1].Stop != "8080" {
		t.Errorf("unexpected second list items: %+v", lists[1].Items)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

// TestListTrafficMatchingListsDecodesNumericAndStringPorts pins the upstream wire
// tolerance: the operator asserts the port variants are JSON numbers, while the
// IP variants are strings. A console that returns either form must decode to the
// same canonical textual value, so a numeric port response does not fail outright.
func TestListTrafficMatchingListsDecodesNumericAndStringPorts(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testTrafficMatchingListID+`","name":"`+testTrafficMatchingListName+`","type":"PORTS",
			 "items":[
			   {"type":"PORT_NUMBER","value":80},
			   {"type":"PORT_NUMBER_RANGE","start":8000,"stop":8080},
			   {"type":"PORT_NUMBER","value":"443"}]}],
			"limit":200,"offset":0,"totalCount":1}`)
	}))

	lists, err := client.ListTrafficMatchingLists(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListTrafficMatchingLists: %v", err)
	}
	if len(lists) != 1 || len(lists[0].Items) != 3 {
		t.Fatalf("lists = %+v, want one list with three items", lists)
	}
	items := lists[0].Items
	if items[0].Value != "80" {
		t.Errorf("numeric port value = %q, want %q", items[0].Value, "80")
	}
	if items[1].Start != "8000" || items[1].Stop != "8080" {
		t.Errorf("numeric port range = %q..%q, want 8000..8080", items[1].Start, items[1].Stop)
	}
	if items[2].Value != "443" {
		t.Errorf("string port value = %q, want %q", items[2].Value, "443")
	}
}

func TestListTrafficMatchingListsUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListTrafficMatchingLists(context.Background(), testSiteID)
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
