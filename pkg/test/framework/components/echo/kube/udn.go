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

package kube

import (
	"encoding/json"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	NetworkStatusAnnotation = "k8s.v1.cni.cncf.io/network-status"
	PodNetworksAnnotation   = "k8s.ovn.org/pod-networks"
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

// GetUDNIPs extracts UDN IPs from pod annotations
// Returns nil if no UDN annotation exists, allowing fallback to status.podIP
func GetUDNIPs(pod *corev1.Pod) []string {
	// Try network-status first (cleaner format)
	if ips := getUDNIPsFromNetworkStatus(pod); len(ips) > 0 {
		return ips
	}

	// Fallback to pod-networks
	if ips := getUDNIPsFromPodNetworks(pod); len(ips) > 0 {
		return ips
	}

	return nil
}

func getUDNIPsFromNetworkStatus(pod *corev1.Pod) []string {
	annotation, ok := pod.Annotations[NetworkStatusAnnotation]
	if !ok {
		return nil
	}

	var interfaces []NetworkInterface
	if err := json.Unmarshal([]byte(annotation), &interfaces); err != nil {
		return nil
	}

	// Find interface marked as default (UDN)
	for _, iface := range interfaces {
		if iface.Default && len(iface.IPs) > 0 {
			return iface.IPs
		}
	}

	return nil
}

func getUDNIPsFromPodNetworks(pod *corev1.Pod) []string {
	annotation, ok := pod.Annotations[PodNetworksAnnotation]
	if !ok {
		return nil
	}

	var networks map[string]PodNetwork
	if err := json.Unmarshal([]byte(annotation), &networks); err != nil {
		return nil
	}

	// Look for primary UDN
	for netName, network := range networks {
		if network.Role == "primary" && netName != "default" {
			return extractIPs(network.IPAddresses)
		}
	}

	// Look for secondary UDN
	for netName, network := range networks {
		if network.Role == "secondary" && netName != "default" {
			return extractIPs(network.IPAddresses)
		}
	}

	return nil
}

func extractIPs(ipCIDRs []string) []string {
	ips := make([]string, 0, len(ipCIDRs))
	for _, ipCIDR := range ipCIDRs {
		// Strip CIDR suffix: "10.10.4.20/24" -> "10.10.4.20"
		ip, _, _ := strings.Cut(ipCIDR, "/")
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips
}
