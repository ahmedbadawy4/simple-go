# simple-go

A small HTTP service written in Go, with everything that takes it from source code to a running Kubernetes pod: unit tests, a container image, a Helm chart, an Argo CD Application and a GitHub Actions pipeline.

It is a reference setup for a Go service on Kubernetes. The service uses only the Go standard library, so there is no `go.sum` and no dependency to update.

## What the service does

| Endpoint | Purpose |
| --- | --- |
| `GET /` | Returns `{"message":"hello world","version":"<build version>"}` |
| `GET /healthz` | Liveness probe. Returns 200 while the process runs. |
| `GET /readyz` | Readiness probe. Returns 200, and 503 after the service receives `SIGTERM`. |
| `GET /metrics` | Metrics in the Prometheus text format. |

- **Metrics:** `http_requests_total{code,method,route}` (counter) and `http_request_duration_seconds{route}` (histogram). The `route` label holds only the registered routes, plus `other` for everything else, so a client cannot create new label values.
- **Logs:** JSON on standard output, written with `log/slog`. Requests to `/` and to unknown paths are logged. Probes and metric scrapes are not.
- **Graceful shutdown:** on `SIGTERM` the service fails `/readyz`, waits 5 seconds so Kubernetes stops sending traffic, then closes the listener and drains open requests for up to 10 seconds. The pod allows 30 seconds in total.
- **Configuration:** the `ADDR` environment variable sets the listen address (default `:8080`). The build argument `VERSION` sets the version that `GET /` returns.

## Run it

With Go 1.27 or newer:

```bash
make test
make run
curl localhost:8080/
```

With only Docker:

```bash
make test-docker
make docker-build
docker run --rm -p 8080:8080 simple-go:dev
```

On a kind cluster, with the image built from this repository:

```bash
make deploy
make smoke
make down
```

To deploy with Argo CD instead of Helm, run `make argocd`. It installs Argo CD v3.6.2 and applies `argocd/application.yaml`, and the cluster then pulls the image from GitHub Container Registry. Use either `make deploy` or `make argocd` on one cluster, not both. Every cluster command in the `Makefile` is pinned to the kubectl context `kind-simple-go`, so it never touches another current context.

Run `make help` for all targets.

## Helm chart

`charts/simple-go` renders a Deployment, a Service and, when enabled, a ServiceMonitor.

- Two replicas by default.
- Pods run as the non-root user 65532, with a read-only root filesystem, no added Linux capabilities, `allowPrivilegeEscalation: false` and the `RuntimeDefault` seccomp profile.
- The readiness probe uses `/readyz` and the liveness probe uses `/healthz`.
- The image tag defaults to the chart `appVersion`.
- `serviceMonitor.enabled=true` adds a ServiceMonitor for the Prometheus Operator. [k8s-observability](https://github.com/ahmedbadawy4/k8s-observability) installs a Prometheus that selects every ServiceMonitor.

| Value | Default | Meaning |
| --- | --- | --- |
| `replicaCount` | `2` | Number of pods |
| `image.repository` | `ghcr.io/ahmedbadawy4/simple-go` | Image repository |
| `image.tag` | `""` | Image tag. Empty means `appVersion`. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `service.port` | `80` | Service port |
| `resources` | 25m CPU and 16Mi memory requested, 64Mi memory limit | Container resources |
| `serviceMonitor.enabled` | `false` | Create a ServiceMonitor |
| `serviceMonitor.interval` | `30s` | Scrape interval |
| `serviceMonitor.labels` | `{}` | Extra labels for the ServiceMonitor |

## Release flow

1. A pull request or a push to `master` runs the pipeline. It builds the image but does not push it.
2. Pushing a tag such as `v0.1.0` pushes `ghcr.io/ahmedbadawy4/simple-go:0.1.0`. The image also gets a `sha-` tag. There is no `latest` tag.
3. Once that image exists, set `appVersion` (and the chart `version`) in `charts/simple-go/Chart.yaml` and push to `master`. Argo CD has automated sync with prune and self-heal turned on, so it rolls out the new image.

GitHub Container Registry creates a new package as private. Set the package to public in its settings, or add an image pull secret to the namespace.

## Pipeline

`.github/workflows/ci.yml` has three jobs.

| Job | What it does |
| --- | --- |
| `test` | Checks formatting with `gofmt`, runs `go vet` and `go test -race`. |
| `chart` | Runs `helm lint`, renders the chart with and without the ServiceMonitor, and validates the output and `argocd/application.yaml` with kubeconform v0.7.0. |
| `image` | Builds the image with Docker Buildx. Pushes it to GitHub Container Registry only for `v*` tags. |

## Repository layout

```text
main.go, app.go, metrics.go   Service code: startup and shutdown, handlers, metrics
main_test.go                  Unit tests for every endpoint and the metrics
Dockerfile                    Multi-stage build, final image is empty apart from the binary
charts/simple-go/             Helm chart
argocd/application.yaml       Argo CD Application
Makefile                      Local tasks, pinned to the kind cluster
.github/workflows/ci.yml      Pipeline
```

## Limits

- The metrics are written by hand and kept in memory per pod, so counters restart from zero when a pod restarts. A larger service would use the Prometheus client library or OpenTelemetry.
- There is no tracing.
- There is no authentication, TLS, Ingress, autoscaling or PodDisruptionBudget. The chart exposes the service inside the cluster only.
