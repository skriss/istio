//go:build integ

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

package pilot

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/ovnk"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = deployment.SingleNamespaceView{}
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// Setup ClusterUserDefinedNetwork (if enabled) before Istio installation
		Setup(func(t resource.Context) error {
			if t.Settings().EnableCUDN {
				return ovnk.Setup(t, t.Settings().CUDNNetworkName, t.Settings().CUDNSelector)
			}
			return nil
		}).
		// Setup Istio with OVN-K UDN support if CUDN is enabled
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			if ctx.Settings().EnableCUDN {
				// Use ControlPlaneValues for all CUDN-related configuration
				cfg.ControlPlaneValues = `
components:
  pilot:
    k8s:
      podAnnotations:
        k8s.ovn.org/open-default-ports: |
          - protocol: tcp
            port: 15017
          - protocol: tcp
            port: 15012
          - protocol: tcp
            port: 443
          - protocol: tcp
            port: 15010
          - protocol: tcp
            port: 15014
  ingressGateways:
  - name: istio-ingressgateway
    enabled: true
    k8s:
      podAnnotations:
        k8s.ovn.org/open-default-ports: |
          - protocol: tcp
            port: 15021
          - protocol: tcp
            port: 15443
          - protocol: tcp
            port: 15012
          - protocol: tcp
            port: 15017
          - protocol: tcp
            port: 15090
values:
  global:
    platform: openshift
  pilot:
    image: quay.io/sridhargaddam/pilot:ovnk-udn-1.28
    env:
      PILOT_ENABLE_OVNK_UDN: "true"
`
			}
		})).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Setup(func(t resource.Context) error {
			gatewayConformanceInputs.Client = t.Clusters().Default()
			gatewayConformanceInputs.Cleanup = !t.Settings().NoCleanup

			return nil
		}).
		Run()
}
