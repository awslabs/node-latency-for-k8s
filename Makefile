SHELL=bash
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
BUILD_DIR_PATH = ${MAKEFILE_PATH}/build
GOOS ?= linux
GOARCH ?= amd64
NLK_KO_DOCKER_REPO ?= ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
KO_DOCKER_REPO = ${NLK_KO_DOCKER_REPO}
WITH_GOFLAGS = KO_DOCKER_REPO=${KO_DOCKER_REPO} GOOS=${GOOS} GOARCH=${GOARCH}
K8S_NODE_LATENCY_IAM_ROLE_ARN ?= arn:aws:iam::${AWS_ACCOUNT_ID}:role/${CLUSTER_NAME}-node-latency-for-k8s
VERSION ?= $(shell git describe --tags --always --dirty)
PREV_VERSION ?= $(shell git describe --abbrev=0 --tags `git rev-list --tags --skip=1 --max-count=1`)

$(shell mkdir -p ${BUILD_DIR_PATH})

toolchain: ## Install toolchain for development
	hack/toolchain.sh

build: ## Build the controller image
	$(eval CONTROLLER_IMG=$(shell $(WITH_GOFLAGS) ko build -B -t $(VERSION) github.com/awslabs/node-latency-for-k8s/cmd/node-latency-for-k8s))
	$(eval CONTROLLER_TAG=$(shell echo ${CONTROLLER_IMG} | sed 's/.*node-latency-for-k8s://' | cut -d'@' -f1))
	$(eval CONTROLLER_DIGEST=$(shell echo ${CONTROLLER_IMG} | sed 's/.*node-latency-for-k8s:.*@//'))
	echo Built ${CONTROLLER_IMG}

publish: verify build ## Build and publish container images and helm chart
	aws ecr-public get-login-password --region us-east-1 | docker login --username AWS --password-stdin ${KO_DOCKER_REPO}
	sed -i.bak "s|repository:.*|repository: $(KO_DOCKER_REPO)/node-latency-for-k8s|" charts/node-latency-for-k8s-chart/values.yaml
	sed -i.bak "s|tag:.*|tag: ${CONTROLLER_TAG}|" charts/node-latency-for-k8s-chart/values.yaml
	sed -i.bak "s|digest:.*|digest: ${CONTROLLER_DIGEST}|" charts/node-latency-for-k8s-chart/values.yaml
	sed -i.bak "s|version:.*|version: $(shell echo ${CONTROLLER_TAG} | tr -d 'v')|" charts/node-latency-for-k8s-chart/Chart.yaml
	sed -i.bak "s|appVersion:.*|appVersion: $(shell echo ${CONTROLLER_TAG} | tr -d 'v')|" charts/node-latency-for-k8s-chart/Chart.yaml
	sed -E -i.bak "s|$(shell echo ${PREV_VERSION} | tr -d 'v')([\"_/])|$(shell echo ${VERSION} | tr -d 'v')\1|g" README.md
	rm -f *.bak charts/node-latency-for-k8s-chart/*.bak
	helm package charts/node-latency-for-k8s-chart -d ${BUILD_DIR_PATH} --version "${VERSION}"
	helm push ${BUILD_DIR_PATH}/node-latency-for-k8s-chart-${VERSION}.tgz "oci://${KO_DOCKER_REPO}"

install:  ## Deploy the latest released version into your ~/.kube/config cluster
	@echo Upgrading to $(shell grep version charts/node-latency-for-k8s-chart/Chart.yaml)
	helm upgrade --install node-latency-for-k8s charts/node-latency-for-k8s-chart --create-namespace --namespace node-latency-for-k8s \
	$(HELM_OPTS)

apply: build ## Deploy the controller from the current state of your git repository into your ~/.kube/config cluster
	helm upgrade --install node-latency-for-k8s charts/node-latency-for-k8s-chart --namespace node-latency-for-k8s --create-namespace \
	$(HELM_OPTS) \
	--set serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=${K8S_NODE_LATENCY_IAM_ROLE_ARN} \
	--set image.repository=$(KO_DOCKER_REPO)/node-latency-for-k8s \
	--set image.digest="$(CONTROLLER_DIGEST)" 

test: build-bin ## local test with docker
	docker build -t nlk-test -f test/Dockerfile .
	docker run -it -v $(shell pwd)/test/not-ready/var/log:/var/log -v ${BUILD_DIR_PATH}/node-latency-for-k8s:/bin/node-latency-for-k8s nlk-test /bin/node-latency-for-k8s --timeout=11 --output=json --no-imds
	docker run -it -v $(shell pwd)/test/normal/var/log:/var/log -v ${BUILD_DIR_PATH}/node-latency-for-k8s:/bin/node-latency-for-k8s nlk-test /bin/node-latency-for-k8s
	docker run -it -v $(shell pwd)/test/no-cni/var/log:/var/log -v ${BUILD_DIR_PATH}/node-latency-for-k8s:/bin/node-latency-for-k8s nlk-test /bin/node-latency-for-k8s --timeout=11 --output=json

verify: licenses ## Run Verifications like helm-lint and govulncheck
	@govulncheck ./pkg/...
	@golangci-lint run
	@helm lint --strict charts/node-latency-for-k8s-chart

docs: ## Generate helm docs
	helm-docs

fmt: ## go fmt the code
	find . -iname "*.go" -exec go fmt {} \;

licenses: ## Verifies dependency licenses
	go mod download
	! go-licenses csv ./... | grep -v -e 'MIT' -e 'Apache-2.0' -e 'BSD-3-Clause' -e 'BSD-2-Clause' -e 'ISC' -e 'MPL-2.0'

help: ## Display help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: verify apply build fmt licenses help test install publish
