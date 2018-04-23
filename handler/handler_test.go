package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/docker/cli/cli/compose/types"
	"github.com/fsouza/go-dockerclient"
	"github.com/go-test/deep"
	"github.com/sirupsen/logrus"

	"github.com/johanbrandhorst/redeploy/config"
	"github.com/johanbrandhorst/redeploy/handler"
)

type createContainerReq struct {
	*docker.Config
	HostConfig       *docker.HostConfig
	NetworkingConfig *docker.NetworkingConfig
}

type checkCalls struct {
	pullCalled     bool
	listCalled     bool
	createCalled   bool
	startCalled    bool
	stopCalled     bool
	removeCalled   bool
	callbackCalled bool
}

func (c checkCalls) Validate() error {
	if !c.pullCalled {
		return fmt.Errorf("Pull not called")
	}
	if !c.listCalled {
		return fmt.Errorf("ListContainers not called")
	}
	if !c.createCalled {
		return fmt.Errorf("CreateContainer not called")
	}
	if !c.startCalled {
		return fmt.Errorf("StartContainer not called")
	}
	if !c.stopCalled {
		return fmt.Errorf("StopContainer not called")
	}
	if !c.removeCalled {
		return fmt.Errorf("RemoveContainer not called")
	}
	if !c.callbackCalled {
		return fmt.Errorf("Success callback not called")
	}

	return nil
}

func TestHandler(t *testing.T) {
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{}

	conf := &config.Config{
		Config: types.Config{
			Version: "3.0",
		},
		Services: []config.Service{
			{
				Name:  "test",
				Image: "test/test1",
			},
		},
	}
	containerOpts, err := conf.Services[0].CreateContainerOptions()
	if err != nil {
		t.Fatal(err)
	}

	checks := checkCalls{}

	s := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		dec := json.NewDecoder(req.Body)
		defer func() {
			err := req.Body.Close()
			if err != nil {
				t.Errorf("Failed to close Body: %v", err)
			}
		}()
		enc := json.NewEncoder(resp)
		switch req.URL.Path {
		case "/_ping":
			t.Log("Got Ping")
		case "/images/create":
			t.Log("Got Pull")
			checks.pullCalled = true
			expected := url.Values{
				"tag":       []string{"latest"},
				"fromImage": []string{"test/test1"},
			}
			if diff := deep.Equal(expected, req.URL.Query()); diff != nil {
				t.Errorf("Unexpected Pull request:\n%v", strings.Join(diff, "\n"))
			}
		case "/containers/json":
			t.Log("Got ListContainers")
			checks.listCalled = true
			expected := url.Values{
				"all": []string{"1"},
			}
			if diff := deep.Equal(expected, req.URL.Query()); diff != nil {
				t.Errorf("Unexpected ListContainers request:\n%v", strings.Join(diff, "\n"))
			}
			err = enc.Encode([]docker.APIContainers{{
				ID:    "1234",
				Names: []string{"/test"},
			}})
			if err != nil {
				t.Error(err)
			}
		case "/containers/create":
			t.Log("Got CreateContainer")
			checks.createCalled = true
			var cr createContainerReq
			err := dec.Decode(&cr)
			if err != nil {
				t.Error(err)
			}
			expected := createContainerReq{
				Config:           containerOpts.Config,
				HostConfig:       containerOpts.HostConfig,
				NetworkingConfig: containerOpts.NetworkingConfig,
			}
			if diff := deep.Equal(expected, cr); diff != nil {
				t.Errorf("Unexpected CreateContainer request:\n%v", strings.Join(diff, "\n"))
			}
			err = enc.Encode(&docker.Container{
				ID: "1234",
			})
			if err != nil {
				t.Error(err)
			}
		case "/version":
			t.Log("Got Version")
			enc := json.NewEncoder(resp)
			err := enc.Encode(map[string]string{
				"ApiVersion": "1.25",
			})
			if err != nil {
				t.Error(err)
			}
		case "/containers/1234/start":
			t.Log("Got StartContainer")
			checks.startCalled = true
		case "/containers/1234/stop":
			t.Log("Got StopContainer")
			checks.stopCalled = true
		case "/containers/1234":
			t.Log("Got RemoveContainer")
			checks.removeCalled = true
		case "/callback":
			t.Log("Got success callback")
			checks.callbackCalled = true
		default:
			t.Errorf("Got unexpected request for path %q", req.URL.Path)
			resp.WriteHeader(http.StatusBadRequest)
			return
		}
	}))
	defer s.Close()

	err = os.Setenv("DOCKER_HOST", s.URL)
	if err != nil {
		t.Fatal(err)
	}

	hook, err := handler.New(conf, handler.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	b := &bytes.Buffer{}
	enc := json.NewEncoder(b)
	err = enc.Encode(&handler.HookRequest{
		CallbackURL: s.URL + "/callback",
		PushData: handler.PushData{
			Tag: "latest",
		},
		Repository: handler.Repository{
			RepoName: "test/test1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("", s.URL, b)

	hook.ServeHTTP(rec, req)

	if err = checks.Validate(); err != nil {
		t.Error(err)
	}
}
