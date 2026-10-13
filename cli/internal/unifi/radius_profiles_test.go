package unifi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	testRadiusProfileID   = "3f2b1a09-8c7d-4e6f-a1b2-c3d4e5f60718"
	testRadiusProfileName = "Corp RADIUS"
)

func TestListRadiusProfilesPaginates(t *testing.T) {
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
				{"id":"`+testRadiusProfileID+`","name":"`+testRadiusProfileName+`","metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
		case "1":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"profile-2","name":"Guest RADIUS","metadata":{"origin":"DERIVED"}}],
				"limit":1,"offset":1,"totalCount":2}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	profiles, err := client.ListRadiusProfiles(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListRadiusProfiles: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/radius/profiles", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want 2: %+v", len(profiles), profiles)
	}
	if profiles[0].ID != testRadiusProfileID || profiles[0].Name != testRadiusProfileName {
		t.Errorf("unexpected first profile: %+v", profiles[0])
	}
	if profiles[0].Metadata.Origin != "USER_DEFINED" || profiles[1].Metadata.Origin != "DERIVED" {
		t.Errorf("unexpected metadata: %+v", profiles)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

// TestListRadiusProfilesIgnoresSecretMaterial pins the security invariant for the
// read-only RADIUS surface: the profile overview models only {id, name, metadata},
// so a response that carries an unexpected credential-like field (for example a
// shared secret) is tolerated but never parsed into the profile a caller could log
// or surface. The frozen v10.4.57 surface exposes no credential field, and the
// client must not accidentally acquire one.
func TestListRadiusProfilesIgnoresSecretMaterial(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-shared-value"
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testRadiusProfileID+`","name":"`+testRadiusProfileName+`","metadata":{"origin":"USER_DEFINED"},
			 "sharedSecret":"`+secret+`","servers":[{"address":"10.0.0.9","secret":"`+secret+`"}]}],
			"limit":1,"offset":0,"totalCount":1}`)
	}))

	profiles, err := client.ListRadiusProfiles(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListRadiusProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != testRadiusProfileID {
		t.Fatalf("profiles = %+v, want one profile %q", profiles, testRadiusProfileID)
	}
	if rendered := fmt.Sprintf("%+v", profiles); strings.Contains(rendered, secret) {
		t.Errorf("secret material reached the parsed profile: %s", rendered)
	}
}

func TestListRadiusProfilesUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"statusName":"UNAUTHORIZED","message":"Missing credentials"}`)
	}))

	_, err := client.ListRadiusProfiles(context.Background(), testSiteID)
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
