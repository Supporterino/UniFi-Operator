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
	testDeviceTagID       = "aa1b2c3d-4e5f-4a6b-7c8d-9e0f1a2b3c4d"
	testDeviceID          = "dd1b2c3d-4e5f-4a6b-7c8d-9e0f1a2b3c4d"
	testOriginUserDefined = "USER_DEFINED"
	testOriginSystem      = "SYSTEM_DEFINED"
)

func TestListDeviceTagsPaginates(t *testing.T) {
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
				{"id":"`+testDeviceTagID+`","name":"core-switch","deviceIds":["`+testDeviceID+`"],"metadata":{"origin":"`+testOriginUserDefined+`"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"tag-2","name":"ap","deviceIds":["dev-a","dev-b"],"metadata":{"origin":"ORCHESTRATED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	tags, err := client.ListDeviceTags(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListDeviceTags: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/device-tags", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(tags) != 2 {
		t.Fatalf("got %d tags, want 2: %+v", len(tags), tags)
	}
	if tags[0].ID != testDeviceTagID || tags[0].Name != "core-switch" {
		t.Errorf("unexpected first tag: %+v", tags[0])
	}
	if len(tags[0].DeviceIDs) != 1 || tags[0].DeviceIDs[0] != testDeviceID {
		t.Errorf("DeviceIDs = %v, want [%s]", tags[0].DeviceIDs, testDeviceID)
	}
	if tags[0].Metadata.Origin != testOriginUserDefined || tags[1].Metadata.Origin != "ORCHESTRATED" {
		t.Errorf("unexpected metadata: %+v", tags)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListDeviceTagsUnauthorizedFailsClosed(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{
			"statusCode":401,"statusName":"UNAUTHORIZED",
			"code":"api.authentication.missing-credentials","message":"Missing credentials",
			"requestId":"3fa85f64-5717-4562-b3fc-2c963f66afa6"}`)
	}))

	_, err := client.ListDeviceTags(context.Background(), testSiteID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Message != testMissingCredentials || apiErr.RequestID != "3fa85f64-5717-4562-b3fc-2c963f66afa6" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.Retryable() {
		t.Errorf("401 must not be retryable")
	}
}
