/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"net/http"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// Shared fixtures for the reconciler tests.
const (
	testSecretName          = "unifi-api-key"
	testSecretKey           = "api-key"
	testSecretValue         = "s3cr3t-api-key"
	testAppVersion          = "10.4.57"
	testControllerName      = "controller"
	testSiteResourceName    = "site-resource"
	testInternalReference   = "default"
	testUpstreamSiteID      = "site-uuid-1"
	testNetworkResourceName = "net"
	statusSubresource       = "status"

	testManagementUnmanaged = "UNMANAGED"
	testUnparseableVersion  = "garbage"
	testHostIPAddress       = "10.0.0.1"
)

// stubUnifiClient is a unifi.Client test double. Every method records its call
// so tests can assert that no upstream mutation happened.
type stubUnifiClient struct {
	mu sync.Mutex

	info    unifi.Info
	infoErr error

	sites    []unifi.Site
	sitesErr error

	networks    []unifi.Network
	networksErr error
	// getNetworkErr, when set, is returned by GetNetwork instead of a lookup.
	getNetworkErr error

	created   unifi.Network
	createErr error
	updated   unifi.Network
	updateErr error

	createRequests []unifi.NetworkRequest
	updateRequests []unifi.NetworkRequest
	// updatedNetworkID records the identifier passed to UpdateNetwork.
	updatedNetworkID string

	deleteErr        error
	deletedSiteID    string
	deletedNetworkID string

	zones    []unifi.FirewallZone
	zonesErr error
	// getZoneErr, when set, is returned by GetZone instead of a lookup.
	getZoneErr error

	createdZone   unifi.FirewallZone
	createZoneErr error
	updatedZone   unifi.FirewallZone
	updateZoneErr error

	zoneCreateRequests []unifi.FirewallZoneRequest
	zoneUpdateRequests []unifi.FirewallZoneRequest
	// updatedZoneID records the identifier passed to UpdateZone.
	updatedZoneID string

	deleteZoneErr     error
	deletedZoneSiteID string
	deletedZoneID     string

	deviceTags    []unifi.DeviceTag
	deviceTagsErr error

	radiusProfiles    []unifi.RadiusProfile
	radiusProfilesErr error

	trafficMatchingLists    []unifi.TrafficMatchingList
	trafficMatchingListsErr error
	// getTrafficMatchingListErr, when set, is returned by GetTrafficMatchingList
	// instead of a lookup.
	getTrafficMatchingListErr error

	createdTrafficMatchingList   unifi.TrafficMatchingList
	createTrafficMatchingListErr error
	updatedTrafficMatchingList   unifi.TrafficMatchingList
	updateTrafficMatchingListErr error

	trafficMatchingListCreateRequests []unifi.TrafficMatchingListRequest
	trafficMatchingListUpdateRequests []unifi.TrafficMatchingListRequest
	// updatedTrafficMatchingListID records the identifier passed to
	// UpdateTrafficMatchingList.
	updatedTrafficMatchingListID string

	deleteTrafficMatchingListErr     error
	deletedTrafficMatchingListSiteID string
	deletedTrafficMatchingListID     string

	firewallPolicies    []unifi.FirewallPolicy
	firewallPoliciesErr error
	// getFirewallPolicyErr, when set, is returned by GetFirewallPolicy instead
	// of a lookup.
	getFirewallPolicyErr error

	createdFirewallPolicy   unifi.FirewallPolicy
	createFirewallPolicyErr error
	updatedFirewallPolicy   unifi.FirewallPolicy
	updateFirewallPolicyErr error

	firewallPolicyCreateRequests []unifi.FirewallPolicyRequest
	firewallPolicyUpdateRequests []unifi.FirewallPolicyRequest
	// updatedFirewallPolicyID records the identifier passed to
	// UpdateFirewallPolicy.
	updatedFirewallPolicyID string

	deleteFirewallPolicyErr     error
	deletedFirewallPolicySiteID string
	deletedFirewallPolicyID     string

	firewallPolicyOrdering    unifi.FirewallPolicyOrdering
	firewallPolicyOrderingErr error

	updatedFirewallPolicyOrdering    unifi.FirewallPolicyOrdering
	updateFirewallPolicyOrderingErr  error
	firewallPolicyOrderingUpdateReqs []unifi.FirewallPolicyOrdering
	// updatedFirewallOrderingSourceZoneID/DestinationZoneID record the zone
	// pair passed to UpdateFirewallPolicyOrdering.
	updatedFirewallOrderingSourceZoneID      string
	updatedFirewallOrderingDestinationZoneID string

	aclRules    []unifi.AclRule
	aclRulesErr error
	// getAclRuleErr, when set, is returned by GetAclRule instead of a lookup.
	getAclRuleErr error

	createdAclRule   unifi.AclRule
	createAclRuleErr error
	updatedAclRule   unifi.AclRule
	updateAclRuleErr error

	aclRuleCreateRequests []unifi.AclRuleRequest
	aclRuleUpdateRequests []unifi.AclRuleRequest
	// updatedAclRuleID records the identifier passed to UpdateAclRule.
	updatedAclRuleID string

	deleteAclRuleErr     error
	deletedAclRuleSiteID string
	deletedAclRuleID     string

	aclRuleOrdering    unifi.AclRuleOrdering
	aclRuleOrderingErr error

	updatedAclRuleOrdering    unifi.AclRuleOrdering
	updateAclRuleOrderingErr  error
	aclRuleOrderingUpdateReqs []unifi.AclRuleOrdering

	dnsPolicies    []unifi.DnsPolicy
	dnsPoliciesErr error
	// getDnsPolicyErr, when set, is returned by GetDnsPolicy instead of a lookup.
	getDnsPolicyErr error

	createdDnsPolicy   unifi.DnsPolicy
	createDnsPolicyErr error
	updatedDnsPolicy   unifi.DnsPolicy
	updateDnsPolicyErr error

	dnsPolicyCreateRequests []unifi.DnsPolicyRequest
	dnsPolicyUpdateRequests []unifi.DnsPolicyRequest
	// updatedDnsPolicyID records the identifier passed to UpdateDnsPolicy.
	updatedDnsPolicyID string

	deleteDnsPolicyErr     error
	deletedDnsPolicySiteID string
	deletedDnsPolicyID     string

	lags    []unifi.Lag
	lagsErr error
	// getLagErr, when set, is returned by GetLag instead of a lookup.
	getLagErr error

	mcLagDomains    []unifi.McLagDomain
	mcLagDomainsErr error
	// getMcLagDomainErr, when set, is returned by GetMcLagDomain instead of a
	// lookup.
	getMcLagDomainErr error

	switchStacks    []unifi.SwitchStack
	switchStacksErr error
	// getSwitchStackErr, when set, is returned by GetSwitchStack instead of a
	// lookup.
	getSwitchStackErr error

	vpnServers    []unifi.VpnServer
	vpnServersErr error

	siteToSiteTunnels    []unifi.SiteToSiteTunnel
	siteToSiteTunnelsErr error

	broadcasts         []unifi.WifiBroadcast
	broadcastOverviews []unifi.WifiBroadcastOverview
	broadcastsErr      error

	createdBroadcast   unifi.WifiBroadcast
	createBroadcastErr error
	updatedBroadcast   unifi.WifiBroadcast
	updateBroadcastErr error

	broadcastCreateRequests []unifi.WifiBroadcastRequest
	broadcastUpdateRequests []unifi.WifiBroadcastRequest
	// updatedBroadcastID records the identifier passed to UpdateWifiBroadcast.
	updatedBroadcastID string

	deleteBroadcastErr     error
	deletedBroadcastSiteID string
	deletedBroadcastID     string

	calls []string
}

func (s *stubUnifiClient) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *stubUnifiClient) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *stubUnifiClient) GetInfo(context.Context) (unifi.Info, error) {
	s.record("GetInfo")
	return s.info, s.infoErr
}

func (s *stubUnifiClient) ListSites(context.Context) ([]unifi.Site, error) {
	s.record("ListSites")
	return s.sites, s.sitesErr
}

func (s *stubUnifiClient) ListNetworks(context.Context, string) ([]unifi.Network, error) {
	s.record("ListNetworks")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.networks, s.networksErr
}

// setNetworks replaces the networks the stub reports, so a test can model the
// console converging on a written state between reconciles.
func (s *stubUnifiClient) setNetworks(networks []unifi.Network) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networks = networks
}

func (s *stubUnifiClient) GetNetwork(_ context.Context, _, networkID string) (unifi.Network, error) {
	s.record("GetNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getNetworkErr != nil {
		return unifi.Network{}, s.getNetworkErr
	}
	for _, network := range s.networks {
		if network.ID == networkID {
			return network, nil
		}
	}
	return unifi.Network{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateNetwork(_ context.Context, _ string, req unifi.NetworkRequest) (unifi.Network, error) {
	s.record("CreateNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createRequests = append(s.createRequests, req)
	return s.created, s.createErr
}

func (s *stubUnifiClient) UpdateNetwork(_ context.Context, _, networkID string, req unifi.NetworkRequest) (unifi.Network, error) {
	s.record("UpdateNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateRequests = append(s.updateRequests, req)
	s.updatedNetworkID = networkID
	return s.updated, s.updateErr
}

func (s *stubUnifiClient) DeleteNetwork(_ context.Context, siteID, networkID string) error {
	s.record("DeleteNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedSiteID = siteID
	s.deletedNetworkID = networkID
	return s.deleteErr
}

func (s *stubUnifiClient) ListZones(context.Context, string) ([]unifi.FirewallZone, error) {
	s.record("ListZones")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zones, s.zonesErr
}

func (s *stubUnifiClient) GetZone(_ context.Context, _, zoneID string) (unifi.FirewallZone, error) {
	s.record("GetZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getZoneErr != nil {
		return unifi.FirewallZone{}, s.getZoneErr
	}
	for _, zone := range s.zones {
		if zone.ID == zoneID {
			return zone, nil
		}
	}
	return unifi.FirewallZone{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateZone(_ context.Context, _ string, req unifi.FirewallZoneRequest) (unifi.FirewallZone, error) {
	s.record("CreateZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zoneCreateRequests = append(s.zoneCreateRequests, req)
	return s.createdZone, s.createZoneErr
}

func (s *stubUnifiClient) UpdateZone(_ context.Context, _, zoneID string, req unifi.FirewallZoneRequest) (unifi.FirewallZone, error) {
	s.record("UpdateZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zoneUpdateRequests = append(s.zoneUpdateRequests, req)
	s.updatedZoneID = zoneID
	return s.updatedZone, s.updateZoneErr
}

func (s *stubUnifiClient) DeleteZone(_ context.Context, siteID, zoneID string) error {
	s.record("DeleteZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedZoneSiteID = siteID
	s.deletedZoneID = zoneID
	return s.deleteZoneErr
}

func (s *stubUnifiClient) ListDeviceTags(context.Context, string) ([]unifi.DeviceTag, error) {
	s.record("ListDeviceTags")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceTags, s.deviceTagsErr
}

func (s *stubUnifiClient) ListRadiusProfiles(context.Context, string) ([]unifi.RadiusProfile, error) {
	s.record("ListRadiusProfiles")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.radiusProfiles, s.radiusProfilesErr
}

func (s *stubUnifiClient) ListTrafficMatchingLists(context.Context, string) ([]unifi.TrafficMatchingList, error) {
	s.record("ListTrafficMatchingLists")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trafficMatchingLists, s.trafficMatchingListsErr
}

func (s *stubUnifiClient) GetTrafficMatchingList(_ context.Context, _, listID string) (unifi.TrafficMatchingList, error) {
	s.record("GetTrafficMatchingList")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getTrafficMatchingListErr != nil {
		return unifi.TrafficMatchingList{}, s.getTrafficMatchingListErr
	}
	for _, list := range s.trafficMatchingLists {
		if list.ID == listID {
			return list, nil
		}
	}
	return unifi.TrafficMatchingList{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateTrafficMatchingList(_ context.Context, _ string, req unifi.TrafficMatchingListRequest) (unifi.TrafficMatchingList, error) {
	s.record("CreateTrafficMatchingList")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficMatchingListCreateRequests = append(s.trafficMatchingListCreateRequests, req)
	return s.createdTrafficMatchingList, s.createTrafficMatchingListErr
}

func (s *stubUnifiClient) UpdateTrafficMatchingList(_ context.Context, _, listID string, req unifi.TrafficMatchingListRequest) (unifi.TrafficMatchingList, error) {
	s.record("UpdateTrafficMatchingList")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficMatchingListUpdateRequests = append(s.trafficMatchingListUpdateRequests, req)
	s.updatedTrafficMatchingListID = listID
	return s.updatedTrafficMatchingList, s.updateTrafficMatchingListErr
}

func (s *stubUnifiClient) DeleteTrafficMatchingList(_ context.Context, siteID, listID string) error {
	s.record("DeleteTrafficMatchingList")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedTrafficMatchingListSiteID = siteID
	s.deletedTrafficMatchingListID = listID
	return s.deleteTrafficMatchingListErr
}

func (s *stubUnifiClient) ListFirewallPolicies(context.Context, string) ([]unifi.FirewallPolicy, error) {
	s.record("ListFirewallPolicies")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firewallPolicies, s.firewallPoliciesErr
}

func (s *stubUnifiClient) GetFirewallPolicy(_ context.Context, _, policyID string) (unifi.FirewallPolicy, error) {
	s.record("GetFirewallPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getFirewallPolicyErr != nil {
		return unifi.FirewallPolicy{}, s.getFirewallPolicyErr
	}
	for _, policy := range s.firewallPolicies {
		if policy.ID == policyID {
			return policy, nil
		}
	}
	return unifi.FirewallPolicy{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateFirewallPolicy(_ context.Context, _ string, req unifi.FirewallPolicyRequest) (unifi.FirewallPolicy, error) {
	s.record("CreateFirewallPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firewallPolicyCreateRequests = append(s.firewallPolicyCreateRequests, req)
	return s.createdFirewallPolicy, s.createFirewallPolicyErr
}

func (s *stubUnifiClient) UpdateFirewallPolicy(_ context.Context, _, policyID string, req unifi.FirewallPolicyRequest) (unifi.FirewallPolicy, error) {
	s.record("UpdateFirewallPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firewallPolicyUpdateRequests = append(s.firewallPolicyUpdateRequests, req)
	s.updatedFirewallPolicyID = policyID
	return s.updatedFirewallPolicy, s.updateFirewallPolicyErr
}

func (s *stubUnifiClient) DeleteFirewallPolicy(_ context.Context, siteID, policyID string) error {
	s.record("DeleteFirewallPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedFirewallPolicySiteID = siteID
	s.deletedFirewallPolicyID = policyID
	return s.deleteFirewallPolicyErr
}

func (s *stubUnifiClient) GetFirewallPolicyOrdering(_ context.Context, _, _, _ string) (unifi.FirewallPolicyOrdering, error) {
	s.record("GetFirewallPolicyOrdering")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firewallPolicyOrdering, s.firewallPolicyOrderingErr
}

func (s *stubUnifiClient) UpdateFirewallPolicyOrdering(_ context.Context, _, sourceZoneID, destinationZoneID string, req unifi.FirewallPolicyOrdering) (unifi.FirewallPolicyOrdering, error) {
	s.record("UpdateFirewallPolicyOrdering")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.firewallPolicyOrderingUpdateReqs = append(s.firewallPolicyOrderingUpdateReqs, req)
	s.updatedFirewallOrderingSourceZoneID = sourceZoneID
	s.updatedFirewallOrderingDestinationZoneID = destinationZoneID
	return s.updatedFirewallPolicyOrdering, s.updateFirewallPolicyOrderingErr
}

func (s *stubUnifiClient) ListAclRules(context.Context, string) ([]unifi.AclRule, error) {
	s.record("ListAclRules")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aclRules, s.aclRulesErr
}

func (s *stubUnifiClient) GetAclRule(_ context.Context, _, ruleID string) (unifi.AclRule, error) {
	s.record("GetAclRule")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getAclRuleErr != nil {
		return unifi.AclRule{}, s.getAclRuleErr
	}
	for _, rule := range s.aclRules {
		if rule.ID == ruleID {
			return rule, nil
		}
	}
	return unifi.AclRule{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateAclRule(_ context.Context, _ string, req unifi.AclRuleRequest) (unifi.AclRule, error) {
	s.record("CreateAclRule")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aclRuleCreateRequests = append(s.aclRuleCreateRequests, req)
	return s.createdAclRule, s.createAclRuleErr
}

func (s *stubUnifiClient) UpdateAclRule(_ context.Context, _, ruleID string, req unifi.AclRuleRequest) (unifi.AclRule, error) {
	s.record("UpdateAclRule")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aclRuleUpdateRequests = append(s.aclRuleUpdateRequests, req)
	s.updatedAclRuleID = ruleID
	return s.updatedAclRule, s.updateAclRuleErr
}

func (s *stubUnifiClient) DeleteAclRule(_ context.Context, siteID, ruleID string) error {
	s.record("DeleteAclRule")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedAclRuleSiteID = siteID
	s.deletedAclRuleID = ruleID
	return s.deleteAclRuleErr
}

func (s *stubUnifiClient) GetAclRuleOrdering(context.Context, string) (unifi.AclRuleOrdering, error) {
	s.record("GetAclRuleOrdering")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aclRuleOrdering, s.aclRuleOrderingErr
}

func (s *stubUnifiClient) UpdateAclRuleOrdering(_ context.Context, _ string, req unifi.AclRuleOrdering) (unifi.AclRuleOrdering, error) {
	s.record("UpdateAclRuleOrdering")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aclRuleOrderingUpdateReqs = append(s.aclRuleOrderingUpdateReqs, req)
	return s.updatedAclRuleOrdering, s.updateAclRuleOrderingErr
}

func (s *stubUnifiClient) ListDnsPolicies(context.Context, string) ([]unifi.DnsPolicy, error) {
	s.record("ListDnsPolicies")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dnsPolicies, s.dnsPoliciesErr
}

func (s *stubUnifiClient) GetDnsPolicy(_ context.Context, _, policyID string) (unifi.DnsPolicy, error) {
	s.record("GetDnsPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getDnsPolicyErr != nil {
		return unifi.DnsPolicy{}, s.getDnsPolicyErr
	}
	for _, policy := range s.dnsPolicies {
		if policy.ID == policyID {
			return policy, nil
		}
	}
	return unifi.DnsPolicy{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateDnsPolicy(_ context.Context, _ string, req unifi.DnsPolicyRequest) (unifi.DnsPolicy, error) {
	s.record("CreateDnsPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dnsPolicyCreateRequests = append(s.dnsPolicyCreateRequests, req)
	return s.createdDnsPolicy, s.createDnsPolicyErr
}

func (s *stubUnifiClient) UpdateDnsPolicy(_ context.Context, _, policyID string, req unifi.DnsPolicyRequest) (unifi.DnsPolicy, error) {
	s.record("UpdateDnsPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dnsPolicyUpdateRequests = append(s.dnsPolicyUpdateRequests, req)
	s.updatedDnsPolicyID = policyID
	return s.updatedDnsPolicy, s.updateDnsPolicyErr
}

func (s *stubUnifiClient) DeleteDnsPolicy(_ context.Context, siteID, policyID string) error {
	s.record("DeleteDnsPolicy")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedDnsPolicySiteID = siteID
	s.deletedDnsPolicyID = policyID
	return s.deleteDnsPolicyErr
}

func (s *stubUnifiClient) ListLags(context.Context, string) ([]unifi.Lag, error) {
	s.record("ListLags")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lags, s.lagsErr
}

func (s *stubUnifiClient) GetLag(_ context.Context, _, lagID string) (unifi.Lag, error) {
	s.record("GetLag")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getLagErr != nil {
		return unifi.Lag{}, s.getLagErr
	}
	for _, lag := range s.lags {
		if lag.ID == lagID {
			return lag, nil
		}
	}
	return unifi.Lag{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) ListMcLagDomains(context.Context, string) ([]unifi.McLagDomain, error) {
	s.record("ListMcLagDomains")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mcLagDomains, s.mcLagDomainsErr
}

func (s *stubUnifiClient) GetMcLagDomain(_ context.Context, _, domainID string) (unifi.McLagDomain, error) {
	s.record("GetMcLagDomain")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getMcLagDomainErr != nil {
		return unifi.McLagDomain{}, s.getMcLagDomainErr
	}
	for _, domain := range s.mcLagDomains {
		if domain.ID == domainID {
			return domain, nil
		}
	}
	return unifi.McLagDomain{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) ListSwitchStacks(context.Context, string) ([]unifi.SwitchStack, error) {
	s.record("ListSwitchStacks")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switchStacks, s.switchStacksErr
}

func (s *stubUnifiClient) GetSwitchStack(_ context.Context, _, stackID string) (unifi.SwitchStack, error) {
	s.record("GetSwitchStack")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getSwitchStackErr != nil {
		return unifi.SwitchStack{}, s.getSwitchStackErr
	}
	for _, stack := range s.switchStacks {
		if stack.ID == stackID {
			return stack, nil
		}
	}
	return unifi.SwitchStack{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) ListVpnServers(context.Context, string) ([]unifi.VpnServer, error) {
	s.record("ListVpnServers")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vpnServers, s.vpnServersErr
}

func (s *stubUnifiClient) ListSiteToSiteTunnels(context.Context, string) ([]unifi.SiteToSiteTunnel, error) {
	s.record("ListSiteToSiteTunnels")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.siteToSiteTunnels, s.siteToSiteTunnelsErr
}

func (s *stubUnifiClient) ListWifiBroadcasts(context.Context, string) ([]unifi.WifiBroadcastOverview, error) {
	s.record("ListWifiBroadcasts")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broadcastOverviews, s.broadcastsErr
}

func (s *stubUnifiClient) GetWifiBroadcast(_ context.Context, _, broadcastID string) (unifi.WifiBroadcast, error) {
	s.record("GetWifiBroadcast")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broadcastsErr != nil {
		return unifi.WifiBroadcast{}, s.broadcastsErr
	}
	for _, broadcast := range s.broadcasts {
		if broadcast.ID == broadcastID {
			return broadcast, nil
		}
	}
	return unifi.WifiBroadcast{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateWifiBroadcast(_ context.Context, _ string, req unifi.WifiBroadcastRequest) (unifi.WifiBroadcast, error) {
	s.record("CreateWifiBroadcast")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastCreateRequests = append(s.broadcastCreateRequests, req)
	return s.createdBroadcast, s.createBroadcastErr
}

func (s *stubUnifiClient) UpdateWifiBroadcast(_ context.Context, _, broadcastID string, req unifi.WifiBroadcastRequest) (unifi.WifiBroadcast, error) {
	s.record("UpdateWifiBroadcast")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastUpdateRequests = append(s.broadcastUpdateRequests, req)
	s.updatedBroadcastID = broadcastID
	return s.updatedBroadcast, s.updateBroadcastErr
}

func (s *stubUnifiClient) DeleteWifiBroadcast(_ context.Context, siteID, broadcastID string) error {
	s.record("DeleteWifiBroadcast")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedBroadcastSiteID = siteID
	s.deletedBroadcastID = broadcastID
	return s.deleteBroadcastErr
}

// setWifiBroadcasts replaces the broadcast details the stub reports, so a test
// can model the console converging on a written state between reconciles.
func (s *stubUnifiClient) setWifiBroadcasts(broadcasts []unifi.WifiBroadcast) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcasts = broadcasts
}

// recordingFactory is a ConnectionFactory that captures the arguments a
// reconciler passes to it and returns a fixed client or error.
type recordingFactory struct {
	mu sync.Mutex

	client   unifi.Client
	err      error
	calls    int
	baseURL  string
	apiKey   string
	insecure bool
}

func (f *recordingFactory) new(baseURL, apiKey string, insecureSkipVerify bool) (unifi.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.baseURL = baseURL
	f.apiKey = apiKey
	f.insecure = insecureSkipVerify
	return f.client, f.err
}

// newTestScheme returns a scheme with the core and UniFi types registered.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add unifi scheme: %v", err)
	}
	return scheme
}

// newFakeClient returns a fake client that counts writes to the status
// subresource so idempotency can be asserted.
func newFakeClient(t *testing.T, statusTypes []client.Object, objects ...client.Object) (client.Client, *int) {
	t.Helper()
	writes := 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context,
				cl client.Client,
				subResourceName string,
				obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				if subResourceName == statusSubresource {
					writes++
				}
				return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		})
	if len(statusTypes) > 0 {
		builder = builder.WithStatusSubresource(statusTypes...)
	}
	return builder.Build(), &writes
}

// apiKeySecret returns a Secret holding the test API key.
func apiKeySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretKey: []byte(testSecretValue)},
	}
}
