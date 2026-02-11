// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"istio.io/istio/pkg/log"
)

const (
	NetworkStatusEnvVar = "NETWORK_STATUS"
	PodNetworksEnvVar   = "POD_NETWORKS"
)

// NetworkInterface represents a network interface from k8s.v1.cni.cncf.io/network-status
type NetworkInterface struct {
	Name      string   `json:"name"`
	Interface string   `json:"interface"`
	IPs       []string `json:"ips"`
	MAC       string   `json:"mac"`
	Default   bool     `json:"default,omitempty"`
	DNS       struct{} `json:"dns"`
}

// PodNetwork represents a network from k8s.ovn.org/pod-networks
type PodNetwork struct {
	IPAddresses []string `json:"ip_addresses"`
	MACAddress  string   `json:"mac_address"`
	GatewayIPs  []string `json:"gateway_ips,omitempty"`
	Role        string   `json:"role"`
}

// getUDNIPs attempts to extract UDN IPs from pod annotations
// Returns nil if no UDN is configured, allowing fallback to INSTANCE_IPS
func getUDNIPs() ([]string, error) {
	// Try network-status annotation first (Multus standard, cleaner format)
	if ips := getUDNIPsFromNetworkStatus(); len(ips) > 0 {
		return ips, nil
	}

	// Fallback to pod-networks annotation (OVN-K specific)
	if ips := getUDNIPsFromPodNetworks(); len(ips) > 0 {
		return ips, nil
	}

	// No UDN found
	return nil, nil
}

// getUDNIPsFromNetworkStatus extracts UDN IPs from k8s.v1.cni.cncf.io/network-status
func getUDNIPsFromNetworkStatus() []string {
	networkStatusJSON := os.Getenv(NetworkStatusEnvVar)
	if networkStatusJSON == "" {
		return nil
	}

	var interfaces []NetworkInterface
	if err := json.Unmarshal([]byte(networkStatusJSON), &interfaces); err != nil {
		log.Warnf("Failed to parse NETWORK_STATUS annotation: %v", err)
		return nil
	}

	// Look for interface marked as default (this is the UDN)
	for _, iface := range interfaces {
		if iface.Default && len(iface.IPs) > 0 {
			log.Infof("Using UDN IPs from network interface '%s' (marked as default): %v",
				iface.Interface, iface.IPs)
			return iface.IPs
		}
	}

	// No default interface found
	return nil
}

// getUDNIPsFromPodNetworks extracts UDN IPs from k8s.ovn.org/pod-networks (fallback)
func getUDNIPsFromPodNetworks() []string {
	podNetworksJSON := os.Getenv(PodNetworksEnvVar)
	if podNetworksJSON == "" {
		return nil
	}

	var networks map[string]PodNetwork
	if err := json.Unmarshal([]byte(podNetworksJSON), &networks); err != nil {
		log.Warnf("Failed to parse POD_NETWORKS annotation: %v", err)
		return nil
	}

	// Look for primary UDN network (role: "primary", not "default")
	for netName, network := range networks {
		if network.Role == "primary" && netName != "default" {
			ips := stripCIDRSuffixes(network.IPAddresses)
			if len(ips) > 0 {
				log.Infof("Using UDN primary network '%s' IPs: %v", netName, ips)
				return ips
			}
		}
	}

	// Look for secondary UDN networks
	for netName, network := range networks {
		if network.Role == "secondary" && netName != "default" {
			ips := stripCIDRSuffixes(network.IPAddresses)
			if len(ips) > 0 {
				log.Infof("Using UDN secondary network '%s' IPs: %v", netName, ips)
				return ips
			}
		}
	}

	return nil
}

// stripCIDRSuffixes removes /24 suffix from IP addresses like "10.10.4.20/24"
func stripCIDRSuffixes(ipCIDRs []string) []string {
	ips := make([]string, 0, len(ipCIDRs))
	for _, ipCIDR := range ipCIDRs {
		ip, _, _ := strings.Cut(ipCIDR, "/")
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips
}

// getInstanceIPs returns the IPs this instance should bind to
// Priority: UDN IPs > INSTANCE_IPS > INSTANCE_IP
func getInstanceIPs() ([]string, error) {
	// Try UDN IPs first (from annotations)
	udnIPs, err := getUDNIPs()
	if err != nil {
		log.Warnf("Error getting UDN IPs: %v, falling back to INSTANCE_IPS", err)
	}
	if len(udnIPs) > 0 {
		return udnIPs, nil
	}

	// Fallback to standard INSTANCE_IPS env var (status.podIPs)
	if ipsEnv := os.Getenv("INSTANCE_IPS"); ipsEnv != "" {
		// Parse status.podIPs format
		ips := parseInstanceIPsFromEnv(ipsEnv)
		if len(ips) > 0 {
			log.Infof("Using INSTANCE_IPS (default network): %v", ips)
			return ips, nil
		}
	}

	// Fallback to single INSTANCE_IP (legacy)
	if ip := os.Getenv("INSTANCE_IP"); ip != "" {
		log.Infof("Using INSTANCE_IP (default network): %s", ip)
		return []string{ip}, nil
	}

	return nil, fmt.Errorf("no instance IPs found in NETWORK_STATUS, POD_NETWORKS, INSTANCE_IPS, or INSTANCE_IP")
}

// parseInstanceIPsFromEnv parses INSTANCE_IPS which comes from status.podIPs
// Format can be JSON array or comma-separated
func parseInstanceIPsFromEnv(ipsEnv string) []string {
	// Try JSON format first: [{"ip":"10.244.0.6"}]
	var podIPs []struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal([]byte(ipsEnv), &podIPs); err == nil {
		ips := make([]string, 0, len(podIPs))
		for _, p := range podIPs {
			if p.IP != "" {
				ips = append(ips, p.IP)
			}
		}
		return ips
	}

	// Fallback to comma-separated format
	ips := strings.Split(ipsEnv, ",")
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			result = append(result, ip)
		}
	}
	return result
}
