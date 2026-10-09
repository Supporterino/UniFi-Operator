package unifi

import (
	"context"
	"fmt"
)

// DeviceTag is a read-only device tag as returned by
// GET /v1/sites/{siteId}/device-tags. Tags group member devices by UUID; the
// tag and device identifiers are upstream UUIDs and must never reach an emitted
// Custom Resource's spec. The frozen v10.4.57 surface exposes tags read-only:
// there is no create, update, delete, or assignment endpoint.
type DeviceTag struct {
	// ID is the tag UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable tag name a selector matches on.
	Name string `json:"name"`
	// DeviceIDs is the UUIDs of the member devices.
	DeviceIDs []string `json:"deviceIds"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// ListDeviceTags returns every device tag on the given site. The snapshot
// commands use it to reverse-map an observed device UUID to a tag name.
func (c *Client) ListDeviceTags(ctx context.Context, siteID string) ([]DeviceTag, error) {
	endpoint, err := c.endpoint("sites", siteID, "device-tags")
	if err != nil {
		return nil, err
	}
	tags, err := listAll[DeviceTag](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list device tags: %w", err)
	}
	return tags, nil
}
