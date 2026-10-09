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
	testZoneID   = "ffcdb32c-6278-4364-8947-df4f77118df8"
	testZoneName = "My custom zone"
)

func TestListZonesPaginates(t *testing.T) {
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
				{"id":"`+testZoneID+`","name":"`+testZoneName+`","networkIds":["`+testNetworkID+`"],"metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"zone-2","name":"Internal","networkIds":[],"metadata":{"origin":"SYSTEM_DEFINED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	zones, err := client.ListZones(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/zones", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(zones) != 2 {
		t.Fatalf("got %d zones, want 2: %+v", len(zones), zones)
	}
	if zones[0].ID != testZoneID || zones[0].Name != testZoneName {
		t.Errorf("unexpected first zone: %+v", zones[0])
	}
	if len(zones[0].NetworkIDs) != 1 || zones[0].NetworkIDs[0] != testNetworkID {
		t.Errorf("NetworkIDs = %v, want [%s]", zones[0].NetworkIDs, testNetworkID)
	}
	if zones[0].Metadata.Origin != "USER_DEFINED" || zones[1].Metadata.Origin != "SYSTEM_DEFINED" {
		t.Errorf("unexpected metadata: %+v", zones)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListZonesUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListZones(context.Background(), testSiteID)
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
