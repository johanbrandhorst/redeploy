## Config

Parses a docker-compose v3 syntax config and outputs it into a fsouza/go-dockerclient compatible format.
The compose file parsing code is greatly inspired by
[`github.com/kubernetes/kompose`'s loader](https://github.com/kubernetes/kompose/tree/master/pkg/loader/compose).
