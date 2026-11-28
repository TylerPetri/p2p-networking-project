LOCAL=127.0.0.1
ADDR=:12345
NAME=Ian
#Vee
#Sydney
TEST_DIR=./internal/dht

run:
	@go run ./cmd/park-node -name ${NAME} -data ~/.p2p-park-${NAME}

bootstrap:
	@go run ./cmd/park-node -name ${NAME} -bootstrap ${LOCAL}${ADDR}

race:
	GOFLAGS=-race go test ./...

racedir:
	GOFLAGS=-race go test -count=1 ${TEST_DIR} -v

cleancache:
	@go clean -cache
# Bounded network lab. Values travel through environment variables, never eval.
# Preserve the existing Park targets above.
export GO LINK PORT BIND ADVERTISE ALLOW_CIDRS DISCOVER_CIDRS STATE
export RATE COUNT NODES ATTEMPTS RESTARTS TIMEOUT JOIN_TIMEOUT DURATION RACE
export ACK_REMOTE ACK_NONLOCAL DROP_EVERY RESPONSE_DELAY SOAK_ROUNDS FUZZ_SECONDS NET_REPORT
GO ?= go
PYTHON ?= python3
NET_REPORT ?= .netlab/report.json

.PHONY: net-help net-host net-join net-attach net-local net-stress net-chaos net-remote net-all net-remote-all net-ci net-qualify
net-help:
	@$(PYTHON) scripts/netlab.py --help
	@echo 'Host: make net-host PORT=45001'
	@echo 'Join: make net-join PORT=45002 LINK=p2p://invite/...'
	@echo 'Same-link checks: make net-attach LINK=p2p://invite/...'
	@echo 'All local suites: make net-all [LINK=p2p://invite/...]'
	@echo 'Remote suites: make net-remote-all LINK=... ACK_REMOTE=1 ALLOW_CIDRS=...'

net-host:
	@$(PYTHON) scripts/netlab.py host
net-join:
	@$(PYTHON) scripts/netlab.py join
net-attach:
	@$(PYTHON) scripts/netlab.py attach --output "$$NET_REPORT"
net-local:
	@$(PYTHON) scripts/netlab.py local --output "$$NET_REPORT"
net-stress:
	@$(PYTHON) scripts/netlab.py local --scenario stress --output "$$NET_REPORT"
net-chaos:
	@$(PYTHON) scripts/netlab.py local --scenario chaos --output "$$NET_REPORT"
net-remote:
	@$(PYTHON) scripts/netlab.py remote --output "$$NET_REPORT"
net-all:
	@$(PYTHON) scripts/netlab_suite.py --output "$$NET_REPORT"
net-remote-all:
	@$(PYTHON) scripts/netlab_suite.py --remote --output "$$NET_REPORT"
net-ci:
	@$(PYTHON) -m unittest discover -s scripts -p 'test_netlab.py'
	@BIND=127.0.0.1:0 ADVERTISE= ACK_NONLOCAL=0 LINK= ALLOW_CIDRS=127.0.0.1/32 DISCOVER_CIDRS= $(PYTHON) scripts/netlab.py local --scenario all --count 3 --rate 10 --output "$$NET_REPORT"
	@BIND=127.0.0.1:0 ADVERTISE= ACK_NONLOCAL=0 LINK= ALLOW_CIDRS=127.0.0.1/32 DISCOVER_CIDRS= $(PYTHON) scripts/check_netlab.py
net-qualify:
	@$(PYTHON) scripts/qualify.py --gate all --output "$$NET_REPORT"
