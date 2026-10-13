package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// Traffic matching item type discriminators, matching the Integration v1
// `IPv4 matching`, `IPv6 matching`, and `Port matching` unions.
const (
	trafficMatchingItemIPAddress      = "IP_ADDRESS"
	trafficMatchingItemIPAddressRange = "IP_ADDRESS_RANGE"
	trafficMatchingItemSubnet         = "SUBNET"
	trafficMatchingItemPortNumber     = "PORT_NUMBER"
	trafficMatchingItemPortRange      = "PORT_NUMBER_RANGE"
)

// minPort and maxPort bound an upstream port number.
const (
	minPort = 1
	maxPort = 65535
)

// TrafficMatchingList is a traffic matching list as returned by the controller.
// ID is an upstream UUID and must be recorded in a Custom Resource's status
// only; the list carries the full item set on both the list and detail
// endpoints.
type TrafficMatchingList struct {
	// ID is the list UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable list name.
	Name string `json:"name"`
	// Type is the list discriminator (IPV4_ADDRESSES, IPV6_ADDRESSES, or
	// PORTS).
	Type string `json:"type"`
	// Items are the list entries. Item values are canonical textual forms.
	Items []TrafficMatchingListItem `json:"items"`
}

// TrafficMatchingListRequest is the create/update traffic-matching-list body.
// It carries only the authored fields {type, name, items}; the list id is
// response-only and is never sent upstream.
type TrafficMatchingListRequest struct {
	// Type is the list discriminator (IPV4_ADDRESSES, IPV6_ADDRESSES, or
	// PORTS).
	Type string `json:"type"`
	// Name is the human-readable list name.
	Name string `json:"name"`
	// Items are the list entries.
	Items []TrafficMatchingListItem `json:"items"`
}

// TrafficMatchingListItem is one entry of a matching list. The upstream item
// union is flattened on the wire as {type, value} or {type, start, stop}, where
// the value is a string for the IP variants and a number for the port variants.
// Value/Start/Stop carry the canonical textual form (a port number is its
// decimal string) so a request and a response compare by value; MarshalJSON
// emits the numeric form for the port variants and UnmarshalJSON normalizes a
// numeric response value to the same textual form.
type TrafficMatchingListItem struct {
	// Type is the item discriminator.
	Type string `json:"type"`
	// Value is the single value (IP address, CIDR subnet, or port number).
	Value string `json:"value,omitempty"`
	// Start is the first value of a range (address or port).
	Start string `json:"start,omitempty"`
	// Stop is the last value of a range (address or port).
	Stop string `json:"stop,omitempty"`
}

// MarshalJSON flattens the item onto the upstream wire shape, emitting a
// numeric value for the port variants. The value is validated so a malformed
// port is a terminal spec error rather than an invalid request body.
func (i TrafficMatchingListItem) MarshalJSON() ([]byte, error) {
	type valueItem struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	type rangeItem struct {
		Type  string `json:"type"`
		Start string `json:"start"`
		Stop  string `json:"stop"`
	}
	switch i.Type {
	case trafficMatchingItemIPAddress, trafficMatchingItemSubnet:
		return json.Marshal(valueItem{Type: i.Type, Value: i.Value})
	case trafficMatchingItemIPAddressRange:
		return json.Marshal(rangeItem{Type: i.Type, Start: i.Start, Stop: i.Stop})
	case trafficMatchingItemPortNumber:
		port, err := parsePort(i.Value)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type  string `json:"type"`
			Value int32  `json:"value"`
		}{Type: i.Type, Value: port})
	case trafficMatchingItemPortRange:
		start, err := parsePort(i.Start)
		if err != nil {
			return nil, err
		}
		stop, err := parsePort(i.Stop)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type  string `json:"type"`
			Start int32  `json:"start"`
			Stop  int32  `json:"stop"`
		}{Type: i.Type, Start: start, Stop: stop})
	default:
		return nil, &InvalidSpecError{Field: "items", Reason: fmt.Sprintf("%q is not a known matching item type", i.Type)}
	}
}

// UnmarshalJSON normalizes an upstream item into the canonical textual form,
// accepting a numeric value for the port variants.
func (i *TrafficMatchingListItem) UnmarshalJSON(data []byte) error {
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
	i.Value = scalarToString(raw.Value)
	i.Start = scalarToString(raw.Start)
	i.Stop = scalarToString(raw.Stop)
	return nil
}

// parsePort parses a decimal port string, bounding it to 1..65535.
func parsePort(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < minPort || n > maxPort {
		return 0, &InvalidSpecError{Field: "items", Reason: fmt.Sprintf("%q is not a valid port number", s)}
	}
	return int32(n), nil
}

// scalarToString renders a JSON string or number as text; a missing field is an
// empty string.
func scalarToString(raw json.RawMessage) string {
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

// ListTrafficMatchingLists returns every traffic matching list on the given
// site.
func (c *HTTPClient) ListTrafficMatchingLists(ctx context.Context, siteID string) ([]TrafficMatchingList, error) {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists")
	if err != nil {
		return nil, err
	}
	return listAll[TrafficMatchingList](ctx, c, endpoint)
}

// GetTrafficMatchingList returns the traffic matching list with the given UUID
// on the given site.
func (c *HTTPClient) GetTrafficMatchingList(ctx context.Context, siteID, listID string) (TrafficMatchingList, error) {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists", listID)
	if err != nil {
		return TrafficMatchingList{}, err
	}
	var list TrafficMatchingList
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &list); err != nil {
		return TrafficMatchingList{}, err
	}
	return list, nil
}

// CreateTrafficMatchingList creates a traffic matching list on the given site.
func (c *HTTPClient) CreateTrafficMatchingList(ctx context.Context, siteID string, req TrafficMatchingListRequest) (TrafficMatchingList, error) {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists")
	if err != nil {
		return TrafficMatchingList{}, err
	}
	var list TrafficMatchingList
	if err := c.do(ctx, http.MethodPost, endpoint, req, &list); err != nil {
		return TrafficMatchingList{}, err
	}
	return list, nil
}

// UpdateTrafficMatchingList replaces the traffic matching list with the given
// UUID on the given site.
func (c *HTTPClient) UpdateTrafficMatchingList(ctx context.Context, siteID, listID string, req TrafficMatchingListRequest) (TrafficMatchingList, error) {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists", listID)
	if err != nil {
		return TrafficMatchingList{}, err
	}
	var list TrafficMatchingList
	if err := c.do(ctx, http.MethodPut, endpoint, req, &list); err != nil {
		return TrafficMatchingList{}, err
	}
	return list, nil
}

// DeleteTrafficMatchingList deletes the traffic matching list with the given
// UUID on the given site.
func (c *HTTPClient) DeleteTrafficMatchingList(ctx context.Context, siteID, listID string) error {
	endpoint, err := c.endpoint("sites", siteID, "traffic-matching-lists", listID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}
