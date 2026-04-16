SHELL := /bin/bash

KIND_CLUSTER ?= snapshot-dev
KIND_CONTEXT ?= kind-$(KIND_CLUSTER)
KUBECTL ?= kubectl --context $(KIND_CONTEXT)
PLUGIN_BIN ?= kubectl-snapshot
LOCAL_BIN ?= $(HOME)/.local/bin
SNAPSHOT_DIR ?= snapshots
LAB_NS ?= sre-lab

.PHONY: help kind-up kind-down build install-plugin plugin-check lab-init lab-clean \
	capture-before capture-after diff analyze \
	scenario-oomkill scenario-crashloop scenario-imagepullbackoff scenario-pending \
	scenario-nodepressure scenario-all scenario-clean status

help:
	@echo "Available targets:"
	@echo "  make kind-up             - Create kind cluster"
	@echo "  make kind-down           - Delete kind cluster"
	@echo "  make build               - Build kubectl-snapshot binary"
	@echo "  make install-plugin      - Install plugin into ~/.local/bin"
	@echo "  make plugin-check        - Validate kubectl sees the plugin"
	@echo "  make lab-init            - Create sre-lab namespace"
	@echo "  make scenario-all        - Apply common SRE failure scenarios"
	@echo "  make scenario-clean      - Remove all injected scenarios"
	@echo "  make capture-before      - Capture baseline snapshot"
	@echo "  make capture-after       - Capture post-fault snapshot"
	@echo "  make diff                - Diff before/after snapshots"
	@echo "  make analyze             - Analyze latest snapshot"
	@echo "  make status              - Show lab pods/events/node conditions"

kind-up:
	kind create cluster --name $(KIND_CLUSTER)
	$(KUBECTL) get nodes

kind-down:
	kind delete cluster --name $(KIND_CLUSTER)

build:
	mkdir -p .gomodcache .gocache
	GOMODCACHE=$$(pwd)/.gomodcache GOCACHE=$$(pwd)/.gocache go build -o $(PLUGIN_BIN) ./cmd/kubectl-snapshot

install-plugin: build
	mkdir -p "$(LOCAL_BIN)"
	cp "$(PLUGIN_BIN)" "$(LOCAL_BIN)/kubectl-snapshot"
	chmod +x "$(LOCAL_BIN)/kubectl-snapshot"
	@echo "Installed to $(LOCAL_BIN)/kubectl-snapshot"
	@echo "Ensure PATH includes $(LOCAL_BIN)"

plugin-check:
	kubectl plugin list | rg snapshot || true
	kubectl snapshot --help

lab-init:
	$(KUBECTL) apply -f scenarios/namespace.yaml

lab-clean:
	-$(KUBECTL) delete ns $(LAB_NS) --wait=false

capture-before:
	mkdir -p $(SNAPSHOT_DIR)
	kubectl snapshot capture --output $(SNAPSHOT_DIR)/before.json

capture-after:
	mkdir -p $(SNAPSHOT_DIR)
	kubectl snapshot capture --output $(SNAPSHOT_DIR)/after.json

diff:
	kubectl snapshot diff $(SNAPSHOT_DIR)/before.json $(SNAPSHOT_DIR)/after.json

analyze:
	kubectl snapshot analyze $(SNAPSHOT_DIR)/after.json

scenario-oomkill: lab-init
	$(KUBECTL) apply -f scenarios/oomkill.yaml

scenario-crashloop: lab-init
	$(KUBECTL) apply -f scenarios/crashloop.yaml

scenario-imagepullbackoff: lab-init
	$(KUBECTL) apply -f scenarios/imagepullbackoff.yaml

scenario-pending: lab-init
	$(KUBECTL) apply -f scenarios/pending-unschedulable.yaml

scenario-nodepressure: lab-init
	@echo "Best-effort DiskPressure trigger. May not always flip Node condition in kind."
	$(KUBECTL) apply -f scenarios/nodepressure-best-effort.yaml

scenario-all: scenario-oomkill scenario-crashloop scenario-imagepullbackoff scenario-pending scenario-nodepressure
	@echo "Waiting for events to accumulate..."
	sleep 20
	$(MAKE) status

scenario-clean:
	-$(KUBECTL) delete -f scenarios/nodepressure-best-effort.yaml --ignore-not-found=true
	-$(KUBECTL) delete -f scenarios/pending-unschedulable.yaml --ignore-not-found=true
	-$(KUBECTL) delete -f scenarios/imagepullbackoff.yaml --ignore-not-found=true
	-$(KUBECTL) delete -f scenarios/crashloop.yaml --ignore-not-found=true
	-$(KUBECTL) delete -f scenarios/oomkill.yaml --ignore-not-found=true

status:
	@echo "=== Pods ($(LAB_NS)) ==="
	-$(KUBECTL) -n $(LAB_NS) get pods -o wide
	@echo
	@echo "=== Recent Warning Events ($(LAB_NS)) ==="
	-$(KUBECTL) -n $(LAB_NS) get events --field-selector type=Warning --sort-by=.lastTimestamp
	@echo
	@echo "=== Node Conditions ==="
	$(KUBECTL) get nodes -o 'custom-columns=NAME:.metadata.name,READY:.status.conditions[?(@.type=="Ready")].status,MEM_PRESSURE:.status.conditions[?(@.type=="MemoryPressure")].status,DISK_PRESSURE:.status.conditions[?(@.type=="DiskPressure")].status,PID_PRESSURE:.status.conditions[?(@.type=="PIDPressure")].status'
