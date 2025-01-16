# Build the manager binary
FROM golang:1.22.10 as builder
WORKDIR /workspace
# Copy the Go Modules and Go source code
COPY ./ ./
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download
# Build
ARG VERSION=main
ARG GIT_COMMIT=HEAD
RUN make build

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/bin/nginx-operator /bin/nginx-operator
USER nonroot:nonroot
ENTRYPOINT ["/bin/nginx-operator"]
