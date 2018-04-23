package handler

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/fsouza/go-dockerclient"
	"github.com/sirupsen/logrus"

	"github.com/johanbrandhorst/redeploy/config"
)

// DockerHook handles incoming requests from the Docker
// webhook API.
type DockerHook struct {
	logger         *logrus.Logger
	client         *docker.Client
	imageToService map[string][]config.Service
}

// DockerHookOption is used to configure specific options
// on the DockerHook struct.
type DockerHookOption func(*DockerHook)

// WithLogger configures the logger to use.
func WithLogger(l *logrus.Logger) DockerHookOption {
	return func(d *DockerHook) {
		d.logger = l
	}
}

// New creates a new DockerHook and connects to
// the docker host. Set DOCKER_HOST to configure
// a custom docker endpoint.
func New(conf *config.Config, opts ...DockerHookOption) (*DockerHook, error) {
	d := &DockerHook{
		imageToService: map[string][]config.Service{},
		logger:         logrus.New(),
	}
	d.logger.Out = ioutil.Discard

	for _, opt := range opts {
		opt(d)
	}

	for _, service := range conf.Services {
		d.imageToService[service.Image] = append(d.imageToService[service.Image], service)

		// Check now so we don't have to check later
		_, err := service.CreateContainerOptions()
		if err != nil {
			return nil, err
		}
	}

	var err error
	d.client, err = docker.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	err = d.client.Ping()
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (h DockerHook) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	var hook HookRequest
	dec := json.NewDecoder(req.Body)
	err := dec.Decode(&hook)
	if err != nil {
		h.logger.WithError(err).Error("Failed to decode request")
		http.Error(resp, "invalid request", http.StatusBadRequest)
		return
	}
	defer func() {
		err = req.Body.Close()
		if err != nil {
			h.logger.WithError(err).Error("Failed to close request body")
			return
		}
	}()

	h.logger.WithField("repo", hook.Repository.RepoURL).Debug("Request received")

	image := hook.Repository.RepoName + ":" + hook.PushData.Tag
	foundServices, ok := h.imageToService[image]
	if !ok && hook.PushData.Tag == "latest" {
		// For images of latest tag, tag is optional.
		foundServices, ok = h.imageToService[hook.Repository.RepoName]
	}
	if !ok {
		h.logger.WithField("image", image).Warn("Got deploy request for image not in config. " +
			"Have you added it to your config?")
		resp.WriteHeader(http.StatusOK)
		_, err = http.Get(hook.CallbackURL)
		if err != nil {
			h.logger.WithError(err).Error("Failed to send success to CallbackURL")
		}
		return
	}

	ctx := context.Background()
	pullOpts := docker.PullImageOptions{
		Repository:   hook.Repository.RepoName,
		Tag:          hook.PushData.Tag,
		Context:      ctx,
		OutputStream: h.logger.Out,
	}

	h.logger.WithField("image", image).Debug("Pulling image")

	err = h.client.PullImage(pullOpts, docker.AuthConfiguration{})
	if err != nil {
		h.logger.WithError(err).Error("Failed to pull image")
		http.Error(resp, "internal error", http.StatusInternalServerError)
		return
	}

	for _, service := range foundServices {
		containers, err := h.client.ListContainers(docker.ListContainersOptions{
			All:     true,
			Context: ctx,
		})
		if err != nil {
			h.logger.WithError(err).Error("Failed to list running containers")
			// Soldier on anyway
		} else {
			h.logger.Debug("Listed running containers")
		}

		var id string
		for _, container := range containers {
			if sliceContains(container.Names, "/"+service.Name) {
				h.logger.WithField("name", service.Name).Debug("Found existing container")
				id = container.ID
				break
			}
		}

		if id != "" {
			// Container with same name exists, stop and remove it
			err = h.client.StopContainerWithContext(id, 10, ctx)
			if err != nil {
				h.logger.WithError(err).Error("Failed to stop running container")
				// Soldier on anyway
			} else {
				h.logger.WithField("name", service.Name).Debug("Stopped existing container")
			}

			err = h.client.RemoveContainer(docker.RemoveContainerOptions{
				ID:      id,
				Context: ctx,
			})
			if err != nil {
				h.logger.WithError(err).Error("Failed to remove existing container")
				// Soldier on anyway
			} else {
				h.logger.WithField("name", service.Name).Debug("Deleted existing container")
			}
		}

		// Error is checked on startup, can't error now.
		cOpts, _ := service.CreateContainerOptions()
		cOpts.Context = ctx

		c, err := h.client.CreateContainer(cOpts)
		if err != nil {
			h.logger.WithError(err).Error("Failed to create new container")
			http.Error(resp, "internal error", http.StatusInternalServerError)
			return
		}

		h.logger.WithField("name", service.Name).Debug("Created container")

		err = h.client.StartContainerWithContext(c.ID, nil, ctx)
		if err != nil {
			h.logger.WithError(err).Error("Failed to start container")
			http.Error(resp, "internal error", http.StatusInternalServerError)
			return
		}

		h.logger.WithField("name", service.Name).Debug("Started container")
	}

	resp.WriteHeader(http.StatusOK)
	_, err = http.Get(hook.CallbackURL)
	if err != nil {
		h.logger.WithError(err).Error("Failed to send success to CallbackURL")
		return
	}

	h.logger.WithField("repo", hook.Repository.RepoName).Debug("Successfully sent callback")
}

func sliceContains(slice []string, in string) bool {
	for _, s := range slice {
		if s == in {
			return true
		}
	}

	return false
}

// HookRequest is the structure of the JSON sent
// with the Docker webhook.
// https://docs.docker.com/docker-hub/webhooks/
type HookRequest struct {
	CallbackURL string     `json:"callback_url"`
	PushData    PushData   `json:"push_data"`
	Repository  Repository `json:"repository"`
}

// PushData contains information about this specific push.
type PushData struct {
	Images   []string `json:"images"`
	PushedAt int      `json:"pushed_at"`
	Pusher   string   `json:"pusher"`
	Tag      string   `json:"tag"`
}

// Repository contains metadata about the repository.
type Repository struct {
	CommentCount    int    `json:"comment_count"`
	DateCreated     int    `json:"date_created"`
	Description     string `json:"description"`
	Dockerfile      string `json:"dockerfile"`
	FullDescription string `json:"full_description"`
	IsOfficial      bool   `json:"is_official"`
	IsPrivate       bool   `json:"is_private"`
	IsTrusted       bool   `json:"is_trusted"`
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	Owner           string `json:"owner"`
	RepoName        string `json:"repo_name"`
	RepoURL         string `json:"repo_url"`
	StarCount       int    `json:"star_count"`
	Status          string `json:"status"`
}
