# Redeploy

Redeploy is a tool for easily and automatically redeploying docker images on a single host.
Perfect for your hobby project where deploying Kubernetes is overkill.
It integrates with Docker Hub automatic builds using the Docker Hub webhook.

Inspired by [docker-hook](https://github.com/schickling/docker-hook).

## Example

Write a configuration file describing which containers to monitor.
Redeploy supports the
[docker-compose v3](https://docs.docker.com/compose/compose-file/)
configuration syntax:

```yaml
version: 3
services:
    grpcweb-example:
        # Overwrite container CMD
        command:
            - "--host"
            - "grpcweb.jbrandhorst.com"
        ports:
            - "443:443"
            - "80:80"
        restart: always
        # This must match the repository name on Docker hub.
        image: jfbrandhorst/grpcweb-example
```

Start the server:

```bash
$ redeploy --config services.yaml --path yourconfigureddockerhubpath
Serving on http://0.0.0.0:8555/yourconfigureddockerhubpath
```

Or even easier; use the docker container!

```bash
$ docker run --rm -d \
    -v $(pwd)/services.yaml:/services.yaml \
    # Mount the docker socket to allow container control.
    # Alternatively, define $DOCKER_HOST to use a remote docker host.
    -v /var/run/docker.sock:/var/run/docker.sock \
    --name redeploy
    -p 8555:8555 \
    jfbrandhorst/redeploy --config /services.yaml --path yourconfigureddockerhubpath
Serving on http://0.0.0.0:8555/yourconfigureddockerhubpath

