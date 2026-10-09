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
	testBroadcastID          = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f"
	testBroadcastName        = "Corp"
	testBroadcastDeviceTagID = "bb1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

func TestListWifiBroadcastsPaginates(t *testing.T) {
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
				{"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","enabled":true,
				 "type":"STANDARD","metadata":{"origin":"USER_DEFINED"},
				 "network":{"type":"SPECIFIC","networkId":"`+testNetworkID+`"},
				 "securityConfiguration":{"type":"WPA2_PERSONAL"},
				 "broadcastingDeviceFilter":{"type":"DEVICE_TAGS","deviceTagIds":["`+testBroadcastDeviceTagID+`"]},
				 "broadcastingFrequenciesGHz":[2.4,5]}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"broadcast-2","name":"IoT","enabled":false,"type":"IOT_OPTIMIZED","metadata":{"origin":"ORCHESTRATED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	broadcasts, err := client.ListWifiBroadcasts(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListWifiBroadcasts: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/wifi/broadcasts", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(broadcasts) != 2 {
		t.Fatalf("got %d broadcasts, want 2: %+v", len(broadcasts), broadcasts)
	}
	first := broadcasts[0]
	if first.ID != testBroadcastID || first.Name != testBroadcastName || first.Type != "STANDARD" {
		t.Errorf("unexpected first broadcast: %+v", first)
	}
	if first.Network == nil || first.Network.NetworkID != testNetworkID {
		t.Errorf("unexpected network reference: %+v", first.Network)
	}
	if first.BroadcastingDeviceFilter == nil || first.BroadcastingDeviceFilter.Type != "DEVICE_TAGS" {
		t.Fatalf("unexpected device filter: %+v", first.BroadcastingDeviceFilter)
	}
	if len(first.BroadcastingFrequenciesGHz) != 2 || first.BroadcastingFrequenciesGHz[0] != 2.4 {
		t.Errorf("BroadcastingFrequenciesGHz = %v", first.BroadcastingFrequenciesGHz)
	}
	if broadcasts[1].Metadata.Origin != "ORCHESTRATED" {
		t.Errorf("metadata = %+v", broadcasts[1].Metadata)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestGetWifiBroadcast(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(APIKeyHeader)
		_, _ = io.WriteString(w, `{
			"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","enabled":true,
			"hideName":false,"clientIsolationEnabled":false,
			"multicastToUnicastConversionEnabled":true,"uapsdEnabled":false,
			"channel2gLockedTo6":false,"dtimPeriod2gLockedTo3":false,
			"type":"STANDARD","metadata":{"origin":"USER_DEFINED"},
			"network":{"type":"SPECIFIC","networkId":"`+testNetworkID+`"},
			"securityConfiguration":{"type":"WPA2_PERSONAL","passphrase":"hunter2","pmfMode":"OPTIONAL"},
			"broadcastingDeviceFilter":{"type":"DEVICE_TAGS","deviceTagIds":["`+testBroadcastDeviceTagID+`"]},
			"advertiseDeviceName":false,"arpProxyEnabled":false,"bssTransitionEnabled":true,
			"broadcastingFrequenciesGHz":[2.4,5]}`)
	}))

	broadcast, err := client.GetWifiBroadcast(context.Background(), testSiteID, testBroadcastID)
	if err != nil {
		t.Fatalf("GetWifiBroadcast: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/wifi/broadcasts/%s", testSiteID, testBroadcastID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if broadcast.ID != testBroadcastID || broadcast.Type != "STANDARD" {
		t.Errorf("unexpected broadcast: %+v", broadcast)
	}
	if broadcast.SecurityConfiguration == nil || broadcast.SecurityConfiguration.Passphrase != "hunter2" {
		t.Fatalf("unexpected security configuration: %+v", broadcast.SecurityConfiguration)
	}
	if broadcast.AdvertiseDeviceName == nil || *broadcast.AdvertiseDeviceName {
		t.Errorf("AdvertiseDeviceName = %v, want pointer to false", broadcast.AdvertiseDeviceName)
	}
	if broadcast.BssTransitionEnabled == nil || !*broadcast.BssTransitionEnabled {
		t.Errorf("BssTransitionEnabled = %v, want pointer to true", broadcast.BssTransitionEnabled)
	}
}

func TestGetWifiBroadcastUnrepresentedSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "unmodeled settings present",
			body: `{
				"id":"` + testBroadcastID + `","name":"` + testBroadcastName + `","enabled":true,
				"type":"STANDARD","metadata":{"origin":"USER_DEFINED"},
				"network":{"type":"SPECIFIC","networkId":"` + testNetworkID + `"},
				"securityConfiguration":{"type":"WPA2_PERSONAL"},
				"basicDataRateKbpsByFrequencyGHz":{"2.4":2000,"5":6000},
				"bandSteeringEnabled":false,
				"mloEnabled":true}`,
			want: []string{"basicDataRateKbpsByFrequencyGHz", "bandSteeringEnabled", "mloEnabled"},
		},
		{
			name: "personal radius configuration present",
			body: `{
				"id":"` + testBroadcastID + `","name":"` + testBroadcastName + `","enabled":true,
				"type":"STANDARD","metadata":{"origin":"USER_DEFINED"},
				"network":{"type":"SPECIFIC","networkId":"` + testNetworkID + `"},
				"securityConfiguration":{"type":"WPA2_PERSONAL",
					"radiusConfiguration":{"nasId":{"type":"USER_DEFINED","value":"nas"},"profileId":"profile-uuid"}}}`,
			want: []string{"securityConfiguration.radiusConfiguration"},
		},
		{
			name: "clean detail",
			body: `{
				"id":"` + testBroadcastID + `","name":"` + testBroadcastName + `","enabled":true,
				"type":"IOT_OPTIMIZED","metadata":{"origin":"USER_DEFINED"},
				"network":{"type":"SPECIFIC","networkId":"` + testNetworkID + `"},
				"securityConfiguration":{"type":"WPA2_PERSONAL"}}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			broadcast, err := client.GetWifiBroadcast(context.Background(), testSiteID, testBroadcastID)
			if err != nil {
				t.Fatalf("GetWifiBroadcast: %v", err)
			}
			got := broadcast.UnrepresentedSettings()
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tt.want) {
				t.Errorf("UnrepresentedSettings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListWifiBroadcastsUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListWifiBroadcasts(context.Background(), testSiteID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}
