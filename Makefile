CLUSTER   ?= simple-go
NAMESPACE ?= simple-go

IMAGE_REPO ?= simple-go
IMAGE_TAG  ?= dev
IMAGE      := $(IMAGE_REPO):$(IMAGE_TAG)

ARGOCD_VERSION ?= v3.6.2
GO_IMAGE       ?= golang:1.27-alpine

# Every cluster command is pinned to the kind cluster, so a different current
# kubectl context can never receive the installs.
CONTEXT := kind-$(CLUSTER)
KUBECTL := kubectl --context $(CONTEXT)
HELM    := helm --kube-context $(CONTEXT)

.DEFAULT_GOAL := help
.PHONY: help fmt vet test test-docker run build docker-build lint render \
        cluster load deploy smoke port-forward argocd down

help: ## Show this list
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

fmt: ## Format the Go code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

test: ## Run the tests (needs Go)
	go test ./...

test-docker: ## Check formatting, run go vet and the tests inside a Go container (needs only Docker)
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMAGE) \
		sh -c 'test -z "$$(gofmt -l .)" && go vet ./... && go test ./...'

run: ## Run the service on http://localhost:8080 (needs Go)
	go run .

build: ## Build the binary into bin/ (needs Go)
	CGO_ENABLED=0 go build -trimpath -o bin/simple-go .

docker-build: ## Build the container image
	docker build --build-arg VERSION=$(IMAGE_TAG) -t $(IMAGE) .

lint: ## Lint and render the Helm chart
	helm lint charts/simple-go
	helm template simple-go charts/simple-go > /dev/null
	helm template simple-go charts/simple-go --set serviceMonitor.enabled=true > /dev/null

render: ## Print the rendered chart
	helm template simple-go charts/simple-go

cluster: ## Create the kind cluster if it does not exist
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --name $(CLUSTER)

load: docker-build cluster ## Build the image and load it into the kind cluster
	kind load docker-image $(IMAGE) --name $(CLUSTER)

deploy: load ## Install the chart into the kind cluster with the local image
	$(HELM) upgrade --install simple-go charts/simple-go \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=$(IMAGE_REPO) --set image.tag=$(IMAGE_TAG) \
		--set image.pullPolicy=Never \
		--wait --timeout 3m

smoke: ## Call /, /healthz and /metrics through the Kubernetes API proxy
	@for p in / /healthz; do \
		echo "GET $$p"; \
		$(KUBECTL) get --raw "/api/v1/namespaces/$(NAMESPACE)/services/simple-go:http/proxy$$p"; \
	done
	@echo "GET /metrics (first lines)"
	@$(KUBECTL) get --raw "/api/v1/namespaces/$(NAMESPACE)/services/simple-go:http/proxy/metrics" | head -n 8

port-forward: ## Open the service on http://localhost:8080
	$(KUBECTL) -n $(NAMESPACE) port-forward svc/simple-go 8080:80

argocd: cluster ## Install Argo CD and the Application (pulls the image from GHCR; use instead of deploy)
	$(KUBECTL) create namespace argocd --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) apply -n argocd --server-side -f https://raw.githubusercontent.com/argoproj/argo-cd/$(ARGOCD_VERSION)/manifests/install.yaml
	$(KUBECTL) -n argocd rollout status deployment/argocd-server --timeout=300s
	$(KUBECTL) apply -f argocd/application.yaml

down: ## Delete the kind cluster
	kind delete cluster --name $(CLUSTER)
