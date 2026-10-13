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
				{"id":"`+testZoneID+`","name":"`+testZoneName+`","networkIds":["`+testNetworkID+`"],"metadata":{"origin":"`+testOriginUserDefined+`"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"zone-2","name":"Internal","networkIds":[],"metadata":{"origin":"`+testOriginSystem+`"}}],
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
	if zones[0].Metadata.Origin != testOriginUserDefined || zones[1].Metadata.Origin != testOriginSystem {
		t.Errorf("unexpected metadata: %+v", zones)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetZone(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{
			"id":"`+testZoneID+`","name":"`+testZoneName+`",
			"networkIds":["`+testNetworkID+`"],"metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	zone, err := client.GetZone(context.Background(), testSiteID, testZoneID)
	if err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/zones/%s", testSiteID, testZoneID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if zone.ID != testZoneID || zone.Name != testZoneName || zone.Metadata.Origin != testOriginUserDefined {
		t.Errorf("unexpected zone: %+v", zone)
	}
}

func TestGetZoneNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"zone not found"}`)
	}))

	_, err := client.GetZone(context.Background(), testSiteID, testZoneID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestCreateZone(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["name"] != testZoneName {
			t.Errorf("name = %v, want %q", body["name"], testZoneName)
		}
		networkIDs, ok := body["networkIds"].([]any)
		if !ok {
			t.Fatalf("networkIds = %v, want a JSON array", body["networkIds"])
		}
		if len(networkIDs) != 1 || networkIDs[0] != testNetworkID {
			t.Errorf("networkIds = %v, want [%s]", networkIDs, testNetworkID)
		}
		for _, key := range []string{"id", testJSONFieldMetadata} {
			if _, ok := body[key]; ok {
				t.Errorf("request must not carry %q: %v", key, body)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testZoneID+`","name":"`+testZoneName+`","networkIds":["`+testNetworkID+`"],"metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	req := FirewallZoneRequest{Name: testZoneName, NetworkIDs: []string{testNetworkID}}
	zone, err := client.CreateZone(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/zones", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if zone.ID != testZoneID {
		t.Errorf("zone ID = %q, want %q", zone.ID, testZoneID)
	}
}

func TestCreateZoneEmptyNetworkIDsMarshalsArray(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		networkIDs, ok := body["networkIds"].([]any)
		if !ok {
			t.Fatalf("networkIds = %v, want a JSON array, not null", body["networkIds"])
		}
		if len(networkIDs) != 0 {
			t.Errorf("networkIds = %v, want empty", networkIDs)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testZoneID+`","name":"empty","networkIds":[],"metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	if _, err := client.CreateZone(context.Background(), testSiteID, FirewallZoneRequest{Name: "empty"}); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
}

func TestUpdateZone(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if _, ok := body[testJSONFieldMetadata]; ok {
			t.Errorf("request must not carry metadata: %v", body)
		}
		_, _ = io.WriteString(w, `{"id":"`+testZoneID+`","name":"`+testZoneName+`","networkIds":[],"metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	req := FirewallZoneRequest{Name: testZoneName}
	if _, err := client.UpdateZone(context.Background(), testSiteID, testZoneID, req); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/zones/%s", testSiteID, testZoneID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteZone(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteZone(context.Background(), testSiteID, testZoneID); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/firewall/zones/%s", testSiteID, testZoneID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestCreateZoneUnauthorizedFailsClosed(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.CreateZone(context.Background(), testSiteID, FirewallZoneRequest{Name: testZoneName})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Retryable() {
		t.Errorf("401 must not be retryable")
	}
}

// writeZBFBadRequest writes the flat Error Message body a console returns when
// Zone-Based Firewall is unconfigured.
func writeZBFBadRequest(w http.ResponseWriter, code, message string) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = fmt.Fprintf(w, `{"statusCode":400,"statusName":"BAD_REQUEST","code":%q,"message":%q}`,
		code, message)
}

func TestListZonesMapsUnconfiguredZBFToEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		code      string
		message   string
		wantEmpty bool
	}{
		{
			name:      "unconfigured code maps to an empty set",
			code:      "api.firewall.zone-based-firewall-not-configured",
			message:   "Zone based firewall is not configured",
			wantEmpty: true,
		},
		{
			name:      "the code alone decides, not the message text",
			code:      "api.firewall.zone-based-firewall-not-configured",
			message:   "something else entirely",
			wantEmpty: true,
		},
		{
			name:      "the same message with another code stays an error",
			code:      "api.firewall.some-other-problem",
			message:   "Zone based firewall is not configured",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeZBFBadRequest(w, tt.code, tt.message)
			}))

			zones, err := client.ListZones(context.Background(), testSiteID)
			if tt.wantEmpty {
				if err != nil {
					t.Fatalf("ListZones = %v, want nil error", err)
				}
				if len(zones) != 0 {
					t.Errorf("zones = %+v, want empty", zones)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Code != tt.code {
				t.Errorf("code = %q, want %q", apiErr.Code, tt.code)
			}
		})
	}
}

func TestGetZoneMapsUnconfiguredZBFToNotFound(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeZBFBadRequest(w, "api.firewall.zone-based-firewall-not-configured",
			"Zone based firewall is not configured")
	}))

	zone, err := client.GetZone(context.Background(), testSiteID, testZoneID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
	if !reflect.DeepEqual(zone, FirewallZone{}) {
		t.Errorf("zone = %+v, want the zero value", zone)
	}
}
