package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/cli/cli/compose/loader"
	"github.com/docker/cli/cli/compose/types"
	"github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

func buildEnvironment() map[string]string {
	env := os.Environ()
	result := make(map[string]string, len(env))
	for _, s := range env {
		// if value is empty, s is like "K=", not "K".
		if !strings.Contains(s, "=") {
			panic(`unexpected environment "` + s + `"`)
		}
		kv := strings.SplitN(s, "=", 2)
		result[kv[0]] = kv[1]
	}
	return result
}

func getWorkdir(inputFile string) (string, error) {
	if strings.Index(inputFile, "/") != 0 {
		workDir, err := os.Getwd()
		if err != nil {
			return "", errors.Wrap(err, "Unable to retrieve config file directory")
		}
		inputFile = filepath.Join(workDir, inputFile)
	}
	return filepath.Dir(inputFile), nil
}

func LoadConfig(filename string) (*Config, error) {
	confData, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	data, err := loader.ParseYAML(confData)
	if err != nil {
		return nil, err
	}

	workdir, err := getWorkdir(filename)
	if err != nil {
		return nil, err
	}

	dockerConfig, err := loader.Load(types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{
			Filename: filename,
			Config:   data,
		}},
		WorkingDir:  workdir,
		Environment: buildEnvironment(),
	})
	if err != nil {
		return nil, err
	}

	config := &Config{
		Config: *dockerConfig,
	}
	for _, service := range dockerConfig.Services {
		config.Services = append(config.Services, Service(service))
	}
	config.Config.Services = nil

	// Sort slice for deterministic output
	sort.Slice(config.Services, func(i, j int) bool {
		return config.Services[i].Name < config.Services[j].Name
	})

	err = config.Validate()
	if err != nil {
		return nil, err
	}

	return config, nil
}

// Config represents a Docker Compose v3 configuration
type Config struct {
	types.Config
	Services []Service
}

// Service represents a Service in a Docker Compose v3 file.
type Service types.ServiceConfig

// Validate checks all required parameters are defined.
func (c *Config) Validate() error {
	for _, service := range c.Services {
		if service.Image == "" {
			return fmt.Errorf("%s: image is required", service.Name)
		}
	}

	return nil
}

// CreateContainerOptions translates the Service configuration to an fsouza/go-dockerclient
// CreateContainerOptions type.
func (s Service) CreateContainerOptions() (docker.CreateContainerOptions, error) {
	c := docker.CreateContainerOptions{
		Name: s.Name,
		Config: &docker.Config{
			Hostname:        s.Hostname,
			Domainname:      s.DomainName,
			User:            s.User,
			StopSignal:      s.StopSignal,
			Cmd:             s.Command,
			DNS:             s.DNS,
			Image:           s.Image,
			WorkingDir:      s.WorkingDir,
			MacAddress:      s.MacAddress,
			Entrypoint:      s.Entrypoint,
			SecurityOpts:    s.SecurityOpt,
			Labels:          s.Labels,
			AttachStderr:    true,
			AttachStdout:    true,
			AttachStdin:     false,
			Tty:             s.Tty,
			OpenStdin:       s.StdinOpen,
			NetworkDisabled: s.NetworkMode == "none",
		},
		HostConfig: &docker.HostConfig{
			CapAdd:          s.CapAdd,
			CapDrop:         s.CapDrop,
			Links:           s.Links,
			DNS:             s.DNS,
			DNSSearch:       s.DNSSearch,
			ExtraHosts:      s.ExtraHosts,
			NetworkMode:     s.NetworkMode,
			IpcMode:         s.Ipc,
			PidMode:         s.Pid,
			SecurityOpt:     s.SecurityOpt,
			CgroupParent:    s.CgroupParent,
			Privileged:      s.Privileged,
			PublishAllPorts: true,
			ReadonlyRootfs:  s.ReadOnly,
		},
	}

	if len(s.Networks) > 0 {
		c.NetworkingConfig = &docker.NetworkingConfig{
			EndpointsConfig: map[string]*docker.EndpointConfig{},
		}
		for name, network := range s.Networks {
			c.NetworkingConfig.EndpointsConfig[name] = &docker.EndpointConfig{
				Aliases:           network.Aliases,
				IPAddress:         network.Ipv4Address,
				GlobalIPv6Address: network.Ipv6Address,
			}
		}
	}

	if len(s.Tmpfs) > 0 {
		c.HostConfig.Tmpfs = map[string]string{}
		for _, tmpfs := range s.Tmpfs {
			c.HostConfig.Tmpfs[tmpfs] = ""
		}
	}

	if len(s.Ulimits) > 0 {
		for name, val := range s.Ulimits {
			c.HostConfig.Ulimits = append(c.HostConfig.Ulimits, docker.ULimit{
				Name: name,
				Soft: int64(val.Soft),
				Hard: int64(val.Hard),
			})
		}
	}

	if len(s.Devices) > 0 {
		for _, device := range s.Devices {
			paths := strings.Split(device, ":")
			if len(paths) != 2 {
				return c, fmt.Errorf("invalid device path: %q", device)
			}
			c.HostConfig.Devices = append(c.HostConfig.Devices, docker.Device{
				PathOnHost:      paths[0],
				PathInContainer: paths[1],
			})
		}
	}

	if s.Logging != nil {
		c.HostConfig.LogConfig = docker.LogConfig{
			Type:   s.Logging.Driver,
			Config: s.Logging.Options,
		}
	}

	if restartPolicy := s.Deploy.RestartPolicy; restartPolicy != nil {
		c.HostConfig.RestartPolicy = docker.RestartPolicy{
			Name: restartPolicy.Condition,
		}

		if restartPolicy.MaxAttempts != nil {
			c.HostConfig.RestartPolicy.MaximumRetryCount = int(*restartPolicy.MaxAttempts)
		}
	} else {
		c.HostConfig.RestartPolicy = docker.RestartPolicy{
			Name: s.Restart,
		}
	}

	if len(s.Volumes) > 0 {
		for _, vol := range s.Volumes {
			hm := docker.HostMount{
				Source:   vol.Source,
				Target:   vol.Target,
				ReadOnly: vol.ReadOnly,
				Type:     vol.Type,
			}

			if vol.Bind != nil {
				hm.BindOptions = &docker.BindOptions{
					Propagation: vol.Bind.Propagation,
				}
			}
			if vol.Tmpfs != nil {
				hm.TempfsOptions = &docker.TempfsOptions{
					SizeBytes: vol.Tmpfs.Size,
				}
			}
			if vol.Volume != nil {
				hm.VolumeOptions = &docker.VolumeOptions{
					NoCopy: vol.Volume.NoCopy,
				}
			}

			c.HostConfig.Mounts = append(c.HostConfig.Mounts, hm)
		}
	}

	if healthCheck := s.HealthCheck; healthCheck != nil && !healthCheck.Disable {
		c.Config.Healthcheck = &docker.HealthConfig{
			Test: healthCheck.Test,
		}

		if healthCheck.Retries != nil {
			c.Config.Healthcheck.Retries = int(*healthCheck.Retries)
		}

		if healthCheck.Timeout != nil {
			c.Config.Healthcheck.Timeout = *healthCheck.Timeout
		}

		if healthCheck.Interval != nil {
			c.Config.Healthcheck.Interval = *healthCheck.Interval
		}

		if healthCheck.StartPeriod != nil {
			c.Config.Healthcheck.StartPeriod = *healthCheck.StartPeriod
		}
	}

	if len(s.Environment) > 0 {
		for key, val := range s.Environment {
			env := key + "="
			if val != nil {
				env += *val
			}
			c.Config.Env = append(c.Config.Env, env)
		}
	}

	if s.StopGracePeriod != nil {
		c.Config.StopTimeout = int(*s.StopGracePeriod)
	}

	if len(s.Ports) > 0 {
		c.HostConfig.PortBindings = map[docker.Port][]docker.PortBinding{}
		for _, portSpec := range s.Ports {
			outside := strconv.Itoa(int(portSpec.Published))
			inside := strconv.Itoa(int(portSpec.Target)) + "/" + portSpec.Protocol
			s := outside + ":" + inside
			c.Config.PortSpecs = append(c.Config.PortSpecs, s)
			c.HostConfig.PortBindings[docker.Port(inside)] = append(
				c.HostConfig.PortBindings[docker.Port(inside)],
				docker.PortBinding{
					HostPort: outside,
				},
			)
		}
	}

	if len(s.Expose) > 0 {
		c.Config.ExposedPorts = map[docker.Port]struct{}{}
		for _, exposeSpec := range s.Expose {
			if !strings.Contains(exposeSpec, "/") {
				exposeSpec += "/tcp"
			}
			c.Config.ExposedPorts[docker.Port(exposeSpec)] = struct{}{}
		}
	}

	if limits := s.Deploy.Resources.Limits; limits != nil {
		c.Config.Memory = int64(limits.MemoryBytes)
		c.HostConfig.Memory = int64(limits.MemoryBytes)
	}

	if reservations := s.Deploy.Resources.Reservations; reservations != nil {
		c.Config.MemoryReservation = int64(reservations.MemoryBytes)
		c.HostConfig.MemoryReservation = int64(reservations.MemoryBytes)
	}

	return c, nil
}
