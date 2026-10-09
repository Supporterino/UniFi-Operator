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
	testBroadcastID           = "bb1b2c3d-4e5f-4a6b-7c8d-9e0f1a2b3c4d"
	testBroadcastName         = "corp"
	testBroadcastTypeStandard = "STANDARD"
	testBroadcastTypeIot      = "IOT_OPTIMIZED"
	testWifiSecurityPersonal  = "WPA2_PERSONAL"
	testWifiNetworkSpecific   = "SPECIFIC"
	testWifiDeviceFilterTags  = "DEVICE_TAGS"
	testWifiActionAllow       = "ALLOW"
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
			_, _ = io.WriteString(w, `{"count":1,"data":[{
				"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","enabled":true,
				"type":"`+testBroadcastTypeStandard+`","metadata":{"origin":"`+testOriginUserDefined+`"},
				"network":{"type":"`+testWifiNetworkSpecific+`","networkId":"`+testNetworkID+`"},
				"securityConfiguration":{"type":"`+testWifiSecurityPersonal+`"},
				"broadcastingDeviceFilter":{"type":"`+testWifiDeviceFilterTags+`","deviceTagIds":["`+testDeviceTagID+`"]},
				"broadcastingFrequenciesGHz":[2.4,5]}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[{
				"id":"broadcast-2","name":"iot","enabled":false,
				"type":"`+testBroadcastTypeIot+`","metadata":{"origin":"ORCHESTRATED"}}],
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
	if first.ID != testBroadcastID || first.Name != testBroadcastName || first.Type != testBroadcastTypeStandard {
		t.Errorf("unexpected first broadcast: %+v", first)
	}
	if first.Network == nil || first.Network.Type != testWifiNetworkSpecific || first.Network.NetworkID != testNetworkID {
		t.Errorf("unexpected network reference: %+v", first.Network)
	}
	if first.BroadcastingDeviceFilter == nil || first.BroadcastingDeviceFilter.Type != testWifiDeviceFilterTags {
		t.Fatalf("unexpected device filter: %+v", first.BroadcastingDeviceFilter)
	}
	if len(first.BroadcastingDeviceFilter.DeviceTagIDs) != 1 || first.BroadcastingDeviceFilter.DeviceTagIDs[0] != testDeviceTagID {
		t.Errorf("DeviceTagIDs = %v, want [%s]", first.BroadcastingDeviceFilter.DeviceTagIDs, testDeviceTagID)
	}
	if len(first.BroadcastingFrequenciesGHz) != 2 || first.BroadcastingFrequenciesGHz[0] != 2.4 || first.BroadcastingFrequenciesGHz[1] != 5 {
		t.Errorf("BroadcastingFrequenciesGHz = %v, want [2.4 5]", first.BroadcastingFrequenciesGHz)
	}
	if broadcasts[1].Metadata.Origin != "ORCHESTRATED" {
		t.Errorf("second broadcast metadata = %+v", broadcasts[1].Metadata)
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
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{
			"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","enabled":true,
			"hideName":false,"clientIsolationEnabled":false,
			"multicastToUnicastConversionEnabled":true,"uapsdEnabled":false,
			"channel2gLockedTo6":false,"dtimPeriod2gLockedTo3":false,
			"type":"`+testBroadcastTypeStandard+`","metadata":{"origin":"`+testOriginUserDefined+`"},
			"network":{"type":"`+testWifiNetworkSpecific+`","networkId":"`+testNetworkID+`"},
			"securityConfiguration":{"type":"`+testWifiSecurityPersonal+`","passphrase":"hunter2",
				"presharedKeys":[{"network":{"type":"`+testWifiNetworkSpecific+`","networkId":"net-2"},"passphrase":"key2"}]},
			"basicDataRateKbpsByFrequencyGHz":{"2.4":2000,"5":6000},
			"dtimPeriodByFrequencyGHzOverride":{"2.4":3,"5":2,"6":2},
			"blackoutScheduleConfiguration":{"days":[{"day":"MON","type":"TIME_RANGE","timeRanges":[{"startTime":"22:00","endTime":"06:00"}]}]},
			"clientFilteringPolicy":{"action":"BLOCK","macAddressFilter":["aa:bb:cc:dd:ee:ff"]},
			"mdnsProxyConfiguration":{"mode":"CUSTOM","policies":[{"action":"`+testWifiActionAllow+`","bridgingNetworkIds":["net-2"],
				"serviceFilter":[{"type":"PREDEFINED","name":"PRINTERS"}]}]},
			"multicastFilteringPolicy":{"action":"`+testWifiActionAllow+`","sourceMacAddressFilter":["01:00:5e:00:00:01"]},
			"dnsAssistanceConfiguration":{"mode":"MANUAL","servers":["1.1.1.1","8.8.8.8"]},
			"handoffSuggestionsConfiguration":{"band5GHzRssiThreshold":-70,"band6GHzRssiThreshold":-80},
			"hotspotConfiguration":{"type":"CAPTIVE_PORTAL"},
			"advertiseDeviceName":false,"arpProxyEnabled":false,"bssTransitionEnabled":true,
			"broadcastingFrequenciesGHz":[2.4,5],"mloEnabled":true}`)
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
	if broadcast.ID != testBroadcastID || broadcast.Network == nil || broadcast.Network.NetworkID != testNetworkID {
		t.Errorf("unexpected broadcast: %+v", broadcast)
	}
	if broadcast.SecurityConfiguration == nil || broadcast.SecurityConfiguration.Passphrase != "hunter2" {
		t.Fatalf("unexpected security configuration: %+v", broadcast.SecurityConfiguration)
	}
	if len(broadcast.SecurityConfiguration.PresharedKeys) != 1 ||
		broadcast.SecurityConfiguration.PresharedKeys[0].Network.NetworkID != "net-2" {
		t.Errorf("unexpected preshared keys: %+v", broadcast.SecurityConfiguration.PresharedKeys)
	}
	if broadcast.BasicDataRateKbpsByFrequencyGHz == nil ||
		broadcast.BasicDataRateKbpsByFrequencyGHz.GHz2_4 != 2000 ||
		broadcast.BasicDataRateKbpsByFrequencyGHz.GHz5 != 6000 {
		t.Errorf("unexpected basic data rates: %+v", broadcast.BasicDataRateKbpsByFrequencyGHz)
	}
	if broadcast.DtimPeriodByFrequencyGHzOverride == nil ||
		broadcast.DtimPeriodByFrequencyGHzOverride.GHz5 != 2 ||
		broadcast.DtimPeriodByFrequencyGHzOverride.GHz6 != 2 {
		t.Errorf("unexpected DTIM override: %+v", broadcast.DtimPeriodByFrequencyGHzOverride)
	}
	if broadcast.MDNSProxyConfiguration == nil || len(broadcast.MDNSProxyConfiguration.Policies) != 1 ||
		len(broadcast.MDNSProxyConfiguration.Policies[0].BridgingNetworkIDs) != 1 {
		t.Errorf("unexpected mDNS configuration: %+v", broadcast.MDNSProxyConfiguration)
	}
	if broadcast.MulticastFilteringPolicy == nil || broadcast.MulticastFilteringPolicy.Action != testWifiActionAllow {
		t.Errorf("unexpected multicast policy: %+v", broadcast.MulticastFilteringPolicy)
	}
	if broadcast.MloEnabled == nil || !*broadcast.MloEnabled {
		t.Errorf("MloEnabled = %v, want true", broadcast.MloEnabled)
	}
}

func TestCreateWifiBroadcast(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body := decodeBody(t, r)
		if body["type"] != testBroadcastTypeStandard || body["name"] != testBroadcastName {
			t.Errorf("unexpected body: %v", body)
		}
		if _, ok := body["standard"]; ok {
			t.Errorf("body must be flat, got nested standard: %v", body["standard"])
		}
		filter, ok := body["broadcastingDeviceFilter"].(map[string]any)
		if !ok || filter["type"] != testWifiDeviceFilterTags {
			t.Errorf("broadcastingDeviceFilter = %v, want DEVICE_TAGS", body["broadcastingDeviceFilter"])
		}
		rates, ok := body["basicDataRateKbpsByFrequencyGHz"].(map[string]any)
		if !ok {
			t.Fatalf("basicDataRateKbpsByFrequencyGHz = %v, want an object", body["basicDataRateKbpsByFrequencyGHz"])
		}
		if rates["2.4"] != float64(2000) || rates["5"] != float64(6000) {
			t.Errorf("basicDataRateKbpsByFrequencyGHz = %v, want exact 2.4/5 keys", rates)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","type":"`+testBroadcastTypeStandard+`","metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	advertise := false
	transition := true
	req := WifiBroadcastRequest{
		Type:                                testBroadcastTypeStandard,
		Name:                                testBroadcastName,
		Enabled:                             true,
		MulticastToUnicastConversionEnabled: true,
		Network:                             &WifiNetworkReference{Type: testWifiNetworkSpecific, NetworkID: testNetworkID},
		SecurityConfiguration:               &WifiSecurityConfiguration{Type: testWifiSecurityPersonal},
		BroadcastingDeviceFilter:            &WifiBroadcastingDeviceFilter{Type: testWifiDeviceFilterTags, DeviceTagIDs: []string{testDeviceTagID}},
		BasicDataRateKbpsByFrequencyGHz:     &WifiBasicDataRateConfiguration{GHz2_4: 2000, GHz5: 6000},
		AdvertiseDeviceName:                 &advertise,
		BssTransitionEnabled:                &transition,
		BroadcastingFrequenciesGHz:          []float64{2.4, 5},
	}
	broadcast, err := client.CreateWifiBroadcast(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateWifiBroadcast: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/wifi/broadcasts", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if broadcast.ID != testBroadcastID {
		t.Errorf("broadcast ID = %q, want %q", broadcast.ID, testBroadcastID)
	}
}

func TestUpdateWifiBroadcast(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if _, ok := decodeBody(t, r)["metadata"]; ok {
			t.Errorf("request must not carry metadata")
		}
		_, _ = io.WriteString(w, `{"id":"`+testBroadcastID+`","metadata":{"origin":"`+testOriginUserDefined+`"}}`)
	}))

	req := WifiBroadcastRequest{Type: testBroadcastTypeIot, Name: testBroadcastName, Enabled: true}
	if _, err := client.UpdateWifiBroadcast(context.Background(), testSiteID, testBroadcastID, req); err != nil {
		t.Fatalf("UpdateWifiBroadcast: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/wifi/broadcasts/%s", testSiteID, testBroadcastID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteWifiBroadcast(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteWifiBroadcast(context.Background(), testSiteID, testBroadcastID); err != nil {
		t.Fatalf("DeleteWifiBroadcast: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/wifi/broadcasts/%s", testSiteID, testBroadcastID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestGetWifiBroadcastNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"broadcast not found"}`)
	}))

	_, err := client.GetWifiBroadcast(context.Background(), testSiteID, testBroadcastID)
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

func TestCreateWifiBroadcastFlatError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"statusCode":400,"statusName":"BAD_REQUEST",
			"code":"api.wifi.invalid-broadcast","message":"invalid broadcast",
			"requestPath":"/integration/v1/sites/123","requestId":"req-1"}`)
	}))

	_, err := client.CreateWifiBroadcast(context.Background(), testSiteID, WifiBroadcastRequest{Type: testBroadcastTypeStandard, Name: testBroadcastName})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Code != "api.wifi.invalid-broadcast" || apiErr.Message != "invalid broadcast" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.Retryable() {
		t.Errorf("400 must not be retryable")
	}
}

func TestListWifiBroadcastsUnauthorizedFailsClosed(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get(apiKeyHeader); got != testAPIKey {
			t.Errorf("api key header = %q, want the configured key", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED",
			"code":"api.authentication.missing-credentials","message":"Missing credentials",
			"requestId":"req-unauthorized"}`)
	}))

	_, err := client.ListWifiBroadcasts(context.Background(), testSiteID)
	if errors.Is(err, ErrNotFound) {
		t.Errorf("401 must not match ErrNotFound")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Message != testMissingCredentials || apiErr.RequestID != "req-unauthorized" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.Retryable() {
		t.Errorf("401 must not be retryable")
	}
	if requests != 1 {
		t.Errorf("requests = %d, want exactly 1 (no credential fallback retry)", requests)
	}
}

func TestGetWifiBroadcastToleratesUnknownFields(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
			"id":"`+testBroadcastID+`","name":"`+testBroadcastName+`","enabled":true,
			"type":"`+testBroadcastTypeStandard+`",
			"metadata":{"origin":"`+testOriginUserDefined+`","futureMetadata":"ignored"},
			"securityConfiguration":{"type":"`+testWifiSecurityPersonal+`","futureSecurityField":{"nested":true}},
			"futureTopLevelField":[1,2,3]}`)
	}))

	broadcast, err := client.GetWifiBroadcast(context.Background(), testSiteID, testBroadcastID)
	if err != nil {
		t.Fatalf("GetWifiBroadcast: %v", err)
	}
	if broadcast.ID != testBroadcastID || broadcast.Name != testBroadcastName || broadcast.Type != testBroadcastTypeStandard {
		t.Errorf("unexpected broadcast: %+v", broadcast)
	}
	if broadcast.SecurityConfiguration == nil || broadcast.SecurityConfiguration.Type != testWifiSecurityPersonal {
		t.Errorf("unexpected security configuration: %+v", broadcast.SecurityConfiguration)
	}
}
