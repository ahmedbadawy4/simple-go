FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/simple-go .

# The binary is static and needs nothing else, so the final image is empty
# apart from it. It runs as a numeric non-root user.
FROM scratch
COPY --from=build /out/simple-go /simple-go
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/simple-go"]
