FROM golang:latest as build

COPY . /go/src/github.com/johanbrandhorst/redeploy
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build -o /redeploy /go/src/github.com/johanbrandhorst/redeploy/main.go

FROM scratch

COPY --from=build /redeploy /redeploy
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

EXPOSE 8555

ENTRYPOINT [ "/redeploy" ]
