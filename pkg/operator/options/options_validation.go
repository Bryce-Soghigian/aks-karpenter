/*
Portions Copyright (c) Microsoft Corporation.

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

package options

import (
	"fmt"
	"net/url"

	"github.com/Azure/karpenter-provider-azure/pkg/consts"
	"github.com/Azure/karpenter-provider-azure/pkg/utils"
	"github.com/go-playground/validator/v10"
	"go.uber.org/multierr"
)

func (o Options) Validate() error {
	validate := validator.New()
	return multierr.Combine(
		o.validateRequiredFields(),
		o.validateEndpoint(),
		o.validateVMMemoryOverheadPercent(),
		o.validateNetworkPluginMode(),
		o.validateNetworkDataplane(),
		o.validateNetworkPlugin(),
		o.validateVnetSubnetID(),
		validate.Struct(o),
	)
}

func (o Options) validateNetworkPluginMode() error {
	if o.NetworkPluginMode != consts.NetworkPluginModeOverlay && o.NetworkPluginMode != consts.NetworkPluginModeNone {
		return fmt.Errorf("network-plugin-mode %v is invalid. network-plugin-mode must equal 'overlay' or ''", o.NetworkPluginMode)
	}
	return nil
}

func (o Options) validateNetworkPlugin() error {
	if o.NetworkPlugin != consts.NetworkPluginAzure && o.NetworkPlugin != consts.NetworkPluginNone {
		return fmt.Errorf("network-plugin %v is invalid. network-plugin must equal 'azure' or 'none'", o.NetworkPlugin)
	}
	return nil
}

func (o Options) validateVnetSubnetID() error {
	_, err := utils.GetVnetSubnetIDComponents(o.SubnetID)
	if err != nil {
		return fmt.Errorf("vnet-subnet-id is invalid: %w", err)
	}
	return nil
}

func (o Options) validateNetworkDataplane() error {
	if o.NetworkDataplane != consts.NetworkDataplaneAzure && o.NetworkDataplane != consts.NetworkDataplaneCilium {
		return fmt.Errorf("network dataplane %s is not a valid network dataplane, valid dataplanes are ('azure', 'cilium')", o.NetworkDataplane)
	}
	return nil
}
func (o Options) validateEndpoint() error {
	if o.ClusterEndpoint == "" {
		return nil
	}
	endpoint, err := url.Parse(o.ClusterEndpoint)
	// url.Parse() will accept a lot of input without error; make
	// sure it's a real URL
	if err != nil || !endpoint.IsAbs() || endpoint.Hostname() == "" {
		return fmt.Errorf("\"%s\" not a valid clusterEndpoint URL", o.ClusterEndpoint)
	}
	return nil
}

func (o Options) validateVMMemoryOverheadPercent() error {
	if o.VMMemoryOverheadPercent < 0 {
		return fmt.Errorf("vm-memory-overhead-percent cannot be negative")
	}
	return nil
}

func (o Options) validateRequiredFields() error {
	if o.ClusterEndpoint == "" {
		return fmt.Errorf("missing field, cluster-endpoint")
	}
	if o.ClusterName == "" {
		return fmt.Errorf("missing field, cluster-name")
	}
	if o.KubeletClientTLSBootstrapToken == "" {
		return fmt.Errorf("missing field, kubelet-bootstrap-token")
	}
	if o.SSHPublicKey == "" {
		return fmt.Errorf("missing field, ssh-public-key")
	}
	if o.SubnetID == "" {
		return fmt.Errorf("missing field, vnet-subnet-id")
	}
	return nil
}
