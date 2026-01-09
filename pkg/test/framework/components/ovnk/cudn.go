//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package ovnk

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
)

const (
	// CUDNAPIVersion is the API version for ClusterUserDefinedNetwork
	CUDNAPIVersion = "k8s.ovn.org/v1"
	// CUDNKind is the kind for ClusterUserDefinedNetwork
	CUDNKind = "ClusterUserDefinedNetwork"
)

var cudnGVR = schema.GroupVersionResource{
	Group:    "k8s.ovn.org",
	Version:  "v1",
	Resource: "clusteruserdefinednetworks",
}

// CUDNManager manages the lifecycle of ClusterUserDefinedNetwork CR for tests
type CUDNManager struct {
	ctx         resource.Context
	networkName string
	selectorKey string
}

// NewCUDNManager creates a new CUDN manager
func NewCUDNManager(ctx resource.Context, networkName, selectorKey string) *CUDNManager {
	return &CUDNManager{
		ctx:         ctx,
		networkName: networkName,
		selectorKey: selectorKey,
	}
}

// CreateOrUpdate creates or updates the ClusterUserDefinedNetwork CR
func (m *CUDNManager) CreateOrUpdate() error {
	if m.networkName == "" {
		return fmt.Errorf("CUDN network name is not set")
	}
	if m.selectorKey == "" {
		return fmt.Errorf("CUDN selector key is not set")
	}

	scopes.Framework.Infof("Creating/Updating ClusterUserDefinedNetwork: %s with selector: %s=true", m.networkName, m.selectorKey)

	cudn := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": CUDNAPIVersion,
			"kind":       CUDNKind,
			"metadata": map[string]interface{}{
				"name": m.networkName,
			},
			"spec": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						m.selectorKey: "true",
					},
				},
				"network": map[string]interface{}{
					"topology": "Layer3",
					"layer3": map[string]interface{}{
						"role": "Primary",
						"subnets": []interface{}{
							map[string]interface{}{
								"cidr": "10.10.0.0/16",
							},
						},
					},
				},
			},
		},
	}

	for _, c := range m.ctx.Clusters() {
		client := c.Dynamic()

		// Try to get existing CUDN
		existing, err := client.Resource(cudnGVR).Get(context.TODO(), m.networkName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Create new CUDN
				_, err = client.Resource(cudnGVR).Create(context.TODO(), cudn, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("failed to create ClusterUserDefinedNetwork in cluster %s: %v", c.Name(), err)
				}
				scopes.Framework.Infof("Created ClusterUserDefinedNetwork %s in cluster %s", m.networkName, c.Name())
			} else {
				return fmt.Errorf("failed to get ClusterUserDefinedNetwork in cluster %s: %v", c.Name(), err)
			}
		} else {
			// Update existing CUDN
			existing.Object["spec"] = cudn.Object["spec"]
			_, err = client.Resource(cudnGVR).Update(context.TODO(), existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update ClusterUserDefinedNetwork in cluster %s: %v", c.Name(), err)
			}
			scopes.Framework.Infof("Updated ClusterUserDefinedNetwork %s in cluster %s", m.networkName, c.Name())
		}
	}

	return nil
}

func (m *CUDNManager) Delete() error {
	if m.networkName == "" {
		return nil
	}

	scopes.Framework.Infof("Deleting ClusterUserDefinedNetwork: %s", m.networkName)

	for _, c := range m.ctx.Clusters() {
		client := c.Dynamic()
		err := client.Resource(cudnGVR).Delete(context.TODO(), m.networkName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			scopes.Framework.Warnf("Failed to delete ClusterUserDefinedNetwork %s in cluster %s: %v", m.networkName, c.Name(), err)
		} else {
			scopes.Framework.Infof("Deleted ClusterUserDefinedNetwork %s from cluster %s", m.networkName, c.Name())
		}
	}

	return nil
}

// Setup is a utility function for setting up ClusterUserDefinedNetwork in a test suite
func Setup(ctx resource.Context, networkName, selectorKey string) error {
	if !ctx.Settings().EnableCUDN {
		scopes.Framework.Info("EnableCUDN is not set, skipping ClusterUserDefinedNetwork creation")
		return nil
	}

	manager := NewCUDNManager(ctx, networkName, selectorKey)

	// Register cleanup
	ctx.CleanupStrategy(cleanup.Conditionally, func() {
		_ = manager.Delete()
	})

	// Create initial CUDN
	return manager.CreateOrUpdate()
}
