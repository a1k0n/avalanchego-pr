// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/types"
	"gopkg.in/yaml.v3"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// Initialize the given path with the docker-compose configuration (compose file and
// volumes) needed for an Antithesis test setup.
func GenerateComposeConfig(
	network *tmpnet.Network,
	nodeImageName string,
	workloadImageName string,
	targetPath string,
) error {
	// Generate a compose project for the specified network
	project, err := newComposeProject(network, nodeImageName, workloadImageName)
	if err != nil {
		return fmt.Errorf("failed to create compose project: %w", err)
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to convert target path to absolute path: %w", err)
	}

	if err := os.MkdirAll(absPath, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create target path %q: %w", absPath, err)
	}

	// Write the compose file
	bytes, err := yaml.Marshal(&project)
	if err != nil {
		return fmt.Errorf("failed to marshal compose project: %w", err)
	}
	composePath := filepath.Join(targetPath, "docker-compose.yml")
	if err := os.WriteFile(composePath, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write genesis: %w", err)
	}

	// Create the volume paths
	for _, service := range project.Services {
		for _, volume := range service.Volumes {
			volumePath := filepath.Join(absPath, volume.Source)
			if err := os.MkdirAll(volumePath, perms.ReadWriteExecute); err != nil {
				return fmt.Errorf("failed to create volume path %q: %w", volumePath, err)
			}
		}
	}
	return nil
}

// Create a new docker compose project for an antithesis test setup
// for the provided network configuration.
func newComposeProject(network *tmpnet.Network, nodeImageName string, workloadImageName string) (*types.Project, error) {
	networkName := "avalanche-testnet"
	baseNetworkAddress := "10.0.20"

	services := make(types.Services, len(network.Nodes)+1)
	uris := make([]string, len(network.Nodes))
	var (
		bootstrapIP  string
		bootstrapIDs string
	)
	for i, node := range network.Nodes {
		address := fmt.Sprintf("%s.%d", baseNetworkAddress, 3+i)

		tlsKey, err := node.Flags.GetStringVal(config.StakingTLSKeyContentKey)
		if err != nil {
			return nil, err
		}
		tlsCert, err := node.Flags.GetStringVal(config.StakingCertContentKey)
		if err != nil {
			return nil, err
		}
		signerKey, err := node.Flags.GetStringVal(config.StakingSignerKeyContentKey)
		if err != nil {
			return nil, err
		}

		env := types.Mapping{
			config.NetworkNameKey:             constants.LocalName,
			config.AdminAPIEnabledKey:         "true",
			config.LogLevelKey:                logging.Debug.String(),
			config.LogDisplayLevelKey:         logging.Trace.String(),
			config.HTTPHostKey:                "0.0.0.0",
			config.PublicIPKey:                address,
			config.StakingTLSKeyContentKey:    tlsKey,
			config.StakingCertContentKey:      tlsCert,
			config.StakingSignerKeyContentKey: signerKey,
		}

		nodeName := "avalanche"
		if i == 0 {
			nodeName += "-bootstrap-node"
			bootstrapIP = address + ":9651"
			bootstrapIDs = node.NodeID.String()
		} else {
			nodeName = fmt.Sprintf("%s-node-%d", nodeName, i+1)
			env[config.BootstrapIPsKey] = bootstrapIP
			env[config.BootstrapIDsKey] = bootstrapIDs
		}

		// The env is defined with the keys and then converted to env
		// vars because only the keys are available as constants.
		env = keyMapToEnvVarMap(env)

		services[i+1] = types.ServiceConfig{
			Name:          nodeName,
			ContainerName: nodeName,
			Hostname:      nodeName,
			Image:         nodeImageName,
			Volumes: []types.ServiceVolumeConfig{
				{
					Type:   types.VolumeTypeBind,
					Source: fmt.Sprintf("./volumes/%s/logs", nodeName),
					Target: "/root/.avalanchego/logs",
				},
			},
			Environment: env.ToMappingWithEquals(),
			Networks: map[string]*types.ServiceNetworkConfig{
				networkName: {
					Ipv4Address: address,
				},
			},
		}

		// Collect URIs for the workload container
		uris[i] = fmt.Sprintf("http://%s:9650", address)
	}

	workloadEnv := types.Mapping{
		"AVAWL_URIS": strings.Join(uris, " "),
	}

	workloadName := "workload"
	services[0] = types.ServiceConfig{
		Name:          workloadName,
		ContainerName: workloadName,
		Hostname:      workloadName,
		Image:         workloadImageName,
		Environment:   workloadEnv.ToMappingWithEquals(),
		Networks: map[string]*types.ServiceNetworkConfig{
			networkName: {
				Ipv4Address: baseNetworkAddress + ".129",
			},
		},
	}

	return &types.Project{
		Networks: types.Networks{
			networkName: types.NetworkConfig{
				Driver: "bridge",
				Ipam: types.IPAMConfig{
					Config: []*types.IPAMPool{
						{
							Subnet: baseNetworkAddress + ".0/24",
						},
					},
				},
			},
		},
		Services: services,
	}, nil
}

// Convert a mapping of avalanche config keys to a mapping of env vars
func keyMapToEnvVarMap(keyMap types.Mapping) types.Mapping {
	envVarMap := make(types.Mapping, len(keyMap))
	for key, val := range keyMap {
		// e.g. network-id -> AVAGO_NETWORK_ID
		envVar := strings.ToUpper(config.EnvPrefix + "_" + config.DashesToUnderscores.Replace(key))
		envVarMap[envVar] = val
	}
	return envVarMap
}
