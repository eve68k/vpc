.PHONY: build test vet lab-image lab-shell netns-up netns-down smoke dhcp-smoke dhcp-smoke

IMAGE ?= vpc-lab

build:
	go build -o bin/vpc-agent ./cmd/vpc-agent

vet:
	go vet ./...

test:
	go test ./...

# --- lab（Linux コンテナ内で netns により PVE を模擬する） ---
lab-image:
	docker build -t $(IMAGE) -f lab/Dockerfile .

lab-shell: lab-image
	docker run --rm -it --privileged -v "$(CURDIR)":/src -w /src $(IMAGE) bash

# 以下は lab-shell（コンテナ内）で実行する
netns-up:
	./lab/up.sh

netns-down:
	./lab/down.sh

smoke:
	./lab/smoke.sh

dhcp-smoke:
	./lab/dhcp-smoke.sh
