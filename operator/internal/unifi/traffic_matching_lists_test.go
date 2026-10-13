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
	testTrafficListID   = "5c4b3a29-1d0e-4f2a-8b7c-6d5e4f3a2b19"
	testTrafficListName = "Allowed ports"
)

func TestListTrafficMatchingListsPaginates(t *testing.T) {
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
				{"id":"`+testTrafficListID+`","name":"`+testTrafficListName+`","type":"PORTS","items":[{"type":"PORT_NUMBER","value":80}]}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"list-2","name":"LAN subnets","type":"IPV4_ADDRESSES","items":[{"type":"SUBNET","value":"192.168.1.0/24"}]}],
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
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if len(lists) != 2 {
		t.Fatalf("got %d lists, want 2: %+v", len(lists), lists)
	}
	if lists[0].ID != testTrafficListID || lists[0].Type != testEndpointPorts {
		t.Errorf("unexpected first list: %+v", lists[0])
	}
	// A numeric upstream port value is normalized to its decimal string form.
	if len(lists[0].Items) != 1 || lists[0].Items[0].Value != "80" {
		t.Errorf("port item = %+v, want value %q", lists[0].Items, "80")
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetTrafficMatchingList(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"id":"`+testTrafficListID+`","name":"`+testTrafficListName+`","type":"IPV4_ADDRESSES","items":[
			{"type":"IP_ADDRESS","value":"192.168.1.5"},
			{"type":"IP_ADDRESS_RANGE","start":"192.168.1.10","stop":"192.168.1.20"}]}`)
	}))

	list, err := client.GetTrafficMatchingList(context.Background(), testSiteID, testTrafficListID)
	if err != nil {
		t.Fatalf("GetTrafficMatchingList: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/traffic-matching-lists/%s", testSiteID, testTrafficListID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if list.ID != testTrafficListID || len(list.Items) != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list.Items[1].Start != testIPv4Address || list.Items[1].Stop != "192.168.1.20" {
		t.Errorf("range item = %+v, want the start/stop strings", list.Items[1])
	}
}

func TestCreateTrafficMatchingList(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["name"] != testTrafficListName || body["type"] != testEndpointPorts {
			t.Errorf("body name/type = %v/%v, want %q/PORTS", body["name"], body["type"], testTrafficListName)
		}
		if _, ok := body["id"]; ok {
			t.Errorf("request must not carry id: %v", body)
		}
		items, ok := body["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("items = %v, want one item", body["items"])
		}
		item, ok := items[0].(map[string]any)
		if !ok {
			t.Fatalf("item = %v, want an object", items[0])
		}
		// A port value must be emitted as a number, not a string.
		if item["type"] != trafficMatchingItemPortNumber || item["value"] != float64(80) {
			t.Errorf("item = %v, want {type: PORT_NUMBER, value: 80}", item)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testTrafficListID+`","name":"`+testTrafficListName+`","type":"PORTS","items":[{"type":"PORT_NUMBER","value":80}]}`)
	}))

	req := TrafficMatchingListRequest{
		Type:  testEndpointPorts,
		Name:  testTrafficListName,
		Items: []TrafficMatchingListItem{{Type: trafficMatchingItemPortNumber, Value: "80"}},
	}
	list, err := client.CreateTrafficMatchingList(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateTrafficMatchingList: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/traffic-matching-lists", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if list.ID != testTrafficListID {
		t.Errorf("list ID = %q, want %q", list.ID, testTrafficListID)
	}
}

func TestUpdateTrafficMatchingList(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		items, ok := body["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("items = %v, want one item", body["items"])
		}
		item, _ := items[0].(map[string]any)
		if item["type"] != "IP_ADDRESS" || item["value"] != "192.168.1.5" {
			t.Errorf("item = %v, want an IP_ADDRESS string value", item)
		}
		_, _ = io.WriteString(w, `{"id":"`+testTrafficListID+`","name":"`+testTrafficListName+`","type":"IPV4_ADDRESSES","items":[{"type":"IP_ADDRESS","value":"192.168.1.5"}]}`)
	}))

	req := TrafficMatchingListRequest{
		Type:  "IPV4_ADDRESSES",
		Name:  testTrafficListName,
		Items: []TrafficMatchingListItem{{Type: "IP_ADDRESS", Value: "192.168.1.5"}},
	}
	if _, err := client.UpdateTrafficMatchingList(context.Background(), testSiteID, testTrafficListID, req); err != nil {
		t.Fatalf("UpdateTrafficMatchingList: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/traffic-matching-lists/%s", testSiteID, testTrafficListID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteTrafficMatchingList(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteTrafficMatchingList(context.Background(), testSiteID, testTrafficListID); err != nil {
		t.Fatalf("DeleteTrafficMatchingList: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/traffic-matching-lists/%s", testSiteID, testTrafficListID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestGetTrafficMatchingListNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"list not found"}`)
	}))

	_, err := client.GetTrafficMatchingList(context.Background(), testSiteID, testTrafficListID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
}

// TestTrafficMatchingListItemMarshalRejectsBadPort pins the port bounds: a
// malformed port value is a terminal spec error rather than an invalid request
// body.
func TestTrafficMatchingListItemMarshalRejectsBadPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item TrafficMatchingListItem
	}{
		{name: "non-numeric", item: TrafficMatchingListItem{Type: trafficMatchingItemPortNumber, Value: "http"}},
		{name: "out of range", item: TrafficMatchingListItem{Type: trafficMatchingItemPortNumber, Value: "70000"}},
		{name: "unknown type", item: TrafficMatchingListItem{Type: "NOPE", Value: "80"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tt.item.MarshalJSON(); err == nil {
				t.Errorf("MarshalJSON(%+v) = nil error, want a terminal error", tt.item)
			}
		})
	}
}
