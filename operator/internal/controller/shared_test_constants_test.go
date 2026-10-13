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

// Shared string constants for the controller test package. They exist so
// repeatedly-used literals are declared once (goconst) without changing any test
// assertion.
const (
	testNameVersionBelowMinimum = "application version below minimum"
	testNameVersionNotReported  = "application version not reported"
	testNameVersionUnparseable  = "version unparseable"
	testNameSiteMissing         = "site missing"
	testNameSiteNotAdopted      = "site not adopted"
	testNameSiteBeingDeleted    = "site being deleted"
	testNameControllerMissing   = "controller missing"
	testActionAllow             = "ALLOW"
	testActionBlock             = "BLOCK"
	testEndpointIPAddress       = "IP_ADDRESS"
	testTypeIPv4                = "IPV4"
	testTypeMac                 = "MAC"
	testEndpointNetworks        = "NETWORKS"
	testEndpointPorts           = "PORTS"
	testIPVersionDual           = "IPV4_AND_IPV6"
	testPortEntryNumber         = "PORT_NUMBER"
	testPortEntryRange          = "PORT_NUMBER_RANGE"
	testIPv4Address             = "192.168.1.10"
	testDnsTypeARecord          = "A_RECORD"
	testDnsTypeSrvRecord        = "SRV_RECORD"
	testSrvProtocol             = "_tcp"
	testSrvServerDomain         = "server.example.com"
	testSrvService              = "_ldap"
	testLagTypeLocal            = "LOCAL"
	testOtherObjectName         = "Other"
	testVpnTypeWireguard        = "WIREGUARD"
	testVpnTypeIpsec            = "IPSEC"
	testEnterpriseSecurity      = "WPA2_ENTERPRISE"
	testSpecFieldName           = "name"
	testSpecFieldSiteRef        = "siteRef"
	testPeerName                = "peer"
	testMissingName             = "missing"
	testReferencingName         = "referencing"
	testRadiusProfileRefName    = "radius"
)
