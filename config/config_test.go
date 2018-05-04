package config_test

import (
	"strings"
	"testing"

	"github.com/fsouza/go-dockerclient"

	"github.com/docker/cli/cli/compose/types"
	"github.com/go-test/deep"

	"github.com/johanbrandhorst/redeploy/config"
)

func getStringReference(in string) *string {
	return &in
}

func TestLoader(t *testing.T) {
	testCases := []struct {
		Name             string
		InputFile        string
		Expected         *config.Config
		ContainerConfigs []docker.CreateContainerOptions
	}{
		{
			Name:      "Simple",
			InputFile: "./testdata/simple.yaml",
			Expected: &config.Config{
				Config: types.Config{
					Version:  "3.0",
					Filename: "./testdata/simple.yaml",
					Networks: map[string]types.NetworkConfig{},
					Volumes:  map[string]types.VolumeConfig{},
					Secrets:  map[string]types.SecretConfig{},
					Configs:  map[string]types.ConfigObjConfig{},
				},
				Services: []config.Service{
					{
						Name:        "test",
						Image:       "test/test1",
						Environment: types.MappingWithEquals{},
					},
				},
			},
			ContainerConfigs: []docker.CreateContainerOptions{{
				Name: "test",
				Config: &docker.Config{
					Image:        "test/test1",
					AttachStderr: true,
					AttachStdout: true,
				},
				HostConfig: &docker.HostConfig{
					PublishAllPorts: true,
				},
			}},
		},
		{
			Name:      "Advanced",
			InputFile: "./testdata/advanced.yaml",
			Expected: &config.Config{
				Config: types.Config{
					Version:  "3.0",
					Filename: "./testdata/advanced.yaml",
					Networks: map[string]types.NetworkConfig{},
					Volumes: map[string]types.VolumeConfig{
						"certs": {},
					},
					Secrets: map[string]types.SecretConfig{},
					Configs: map[string]types.ConfigObjConfig{},
				},
				Services: []config.Service{
					{
						Name:  "chronic-pain-tracker",
						Image: "jfbrandhorst/chronic-pain-tracker",
						Environment: types.MappingWithEquals{
							"POSTGRES_URL": getStringReference("postgres://postgres:ladida@postgres:5432/postgres"),
						},
						Links: []string{"postgres"},
						Ports: []types.ServicePortConfig{{
							Mode:      "ingress",
							Target:    8080,
							Published: 8080,
							Protocol:  "tcp",
						}},
						Restart: "always",
					},
					{
						Name:        "grpcweb-example",
						Image:       "jfbrandhorst/grpcweb-example",
						Environment: types.MappingWithEquals{},
						Ports: []types.ServicePortConfig{
							{
								Mode:      "ingress",
								Target:    443,
								Published: 443,
								Protocol:  "tcp",
							},
							{
								Mode:      "ingress",
								Target:    80,
								Published: 80,
								Protocol:  "tcp",
							},
						},
						Restart: "always",
						Command: types.ShellCommand{
							"--host",
							"grpcweb.jbrandhorst.com",
						},
						Volumes: []types.ServiceVolumeConfig{
							{
								Type:   "volume",
								Source: "certs",
								Target: "/certs",
							},
						},
					},
				},
			},
			ContainerConfigs: []docker.CreateContainerOptions{
				{
					Name: "chronic-pain-tracker",
					Config: &docker.Config{
						Image: "jfbrandhorst/chronic-pain-tracker",
						Env: []string{
							"POSTGRES_URL=postgres://postgres:ladida@postgres:5432/postgres",
						},
						PortSpecs:    []string{"8080:8080/tcp"},
						AttachStderr: true,
						AttachStdout: true,
					},
					HostConfig: &docker.HostConfig{
						Links: []string{"postgres"},
						PortBindings: map[docker.Port][]docker.PortBinding{
							docker.Port("8080/tcp"): {{
								HostPort: "8080",
							}},
						},
						RestartPolicy:   docker.AlwaysRestart(),
						PublishAllPorts: true,
					},
				},
				{
					Name: "grpcweb-example",
					Config: &docker.Config{
						Image: "jfbrandhorst/grpcweb-example",
						Cmd: []string{
							"--host",
							"grpcweb.jbrandhorst.com",
						},
						PortSpecs: []string{
							"443:443/tcp",
							"80:80/tcp",
						},
						AttachStderr: true,
						AttachStdout: true,
					},
					HostConfig: &docker.HostConfig{
						PortBindings: map[docker.Port][]docker.PortBinding{
							docker.Port("443/tcp"): {{
								HostPort: "443",
							}},
							docker.Port("80/tcp"): {{
								HostPort: "80",
							}},
						},
						RestartPolicy: docker.AlwaysRestart(),
						Mounts: []docker.HostMount{{
							Source: "certs",
							Target: "/certs",
							Type:   "volume",
						}},
						PublishAllPorts: true,
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		c, err := config.LoadConfig(testCase.InputFile)
		if err != nil {
			t.Errorf("Error parsing test file: %v", err)
			continue
		}

		if diff := deep.Equal(c, testCase.Expected); diff != nil {
			t.Errorf("For %s:\n%v", testCase.Name, strings.Join(diff, "\n"))
			continue
		}

		for i, service := range c.Services {
			opts, err := service.CreateContainerOptions()
			if err != nil {
				t.Errorf("Error getting service container options: %v", err)
				continue
			}

			if diff := deep.Equal(opts, testCase.ContainerConfigs[i]); diff != nil {
				t.Errorf("For %s #%d:\n%v", testCase.Name, i+1, strings.Join(diff, "\n"))
				continue
			}
		}
	}
}
