package unifi

import (
	"context"
	"encoding/json"
	"fmt"
)

// TrafficMatchingItem is one entry of a traffic matching list as returned by
// GET /v1/sites/{siteId}/traffic-matching-lists. The upstream schema is a
// type-discriminated item: Type is IP_ADDRESS, IP_ADDRESS_RANGE, SUBNET,
// PORT_NUMBER, or PORT_NUMBER_RANGE. Value holds the scalar for the
// value-bearing variants (IP_ADDRESS, SUBNET, PORT_NUMBER); Start/Stop hold the
// bounds for the range variants (IP_ADDRESS_RANGE, PORT_NUMBER_RANGE).
//
// The upstream wire shape is a string for the IP variants and a JSON number for
// the port variants (the operator asserts this by serving numeric ports).
// UnmarshalJSON normalizes either form to the canonical textual value so a
// numeric port response does not fail to decode.
type TrafficMatchingItem struct {
	// Type is the item discriminator.
	Type string `json:"type"`
	// Value is the item value for scalar variants.
	Value string `json:"value,omitempty"`
	// Start is the first bound of a range variant.
	Start string `json:"start,omitempty"`
	// Stop is the last bound of a range variant.
	Stop string `json:"stop,omitempty"`
}

// UnmarshalJSON normalizes an upstream item into the canonical textual form,
// accepting a numeric value for the port variants as well as a string.
func (i *TrafficMatchingItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
		Start json.RawMessage `json:"start"`
		Stop  json.RawMessage `json:"stop"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.Type = raw.Type
	i.Value = trafficMatchingScalar(raw.Value)
	i.Start = trafficMatchingScalar(raw.Start)
	i.Stop = trafficMatchingScalar(raw.Stop)
	return nil
}

// trafficMatchingScalar renders a JSON string or number as text; a missing field
// is an empty string.
func trafficMatchingScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

// TrafficMatchingList is a reusable list of IP addresses, IPv6 addresses, or
// ports as returned by GET /v1/sites/{siteId}/traffic-matching-lists. The list
// endpoint returns the items inline, so no detail call is needed. ID is an
// upstream UUID and must never reach an emitted Custom Resource's spec; the
// list's entries are typed values and ranges and carry no opaque identifier.
type TrafficMatchingList struct {
	// ID is the list UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable list name.
	Name string `json:"name"`
	// Type is the list discriminator: IPV4_ADDRESSES, IPV6_ADDRESSES, or PORTS.
	Type string `json:"type"`
	// Items are the typed list entries.
	Items []TrafficMatchingItem `json:"items"`
}

// ListTrafficMatchingLists returns every traffic matching list on the given
// site. The list endpoint returns each list's items inline, so it is the only
// read needed to project a list into a Custom Resource; there is deliberately
// no detail method.
func (c *Client) ListTrafficMatchingLists(ctx context.Context, siteID string) ([]TrafficMatchingList, error) {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists")
	if err != nil {
		return nil, err
	}
	lists, err := listAll[TrafficMatchingList](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list traffic matching lists: %w", err)
	}
	return lists, nil
}
