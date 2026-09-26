SHELL := /bin/bash
.DEFAULT_GOAL := help
COMPOSE := docker compose -f docker-compose.yml
ENV_FILE ?= .env
export ENV_FILE
WEBHOOK_URL ?= https://searchingly-encouraged-topi.cloudpub.ru/webhook
FRONT_DIR ?= ../DownloaderPRMYVERSIONFront

.PHONY: help init deps build run up down restart logs ps check fmt health webhook-set webhook-info webhook-delete front-up front-down front-logs
help:
	@printf '%s\n' 'make init / deps       — подготовить env / Go-зависимости' 'make up / down        — собрать и запустить / остановить backend' 'make restart / logs / ps' 'make build / run      — локальная сборка / запуск' 'make fmt / check      — форматирование / проверка сборки и vet' 'make health           — проверить HTTP' 'make webhook-set WEBHOOK_URL=https://host/webhook' 'make webhook-info / webhook-delete' 'make front-up / front-down / front-logs — отдельный UI-проект'
init:
	@test -f "$(ENV_FILE)" || (umask 077; cp .env.example "$(ENV_FILE)")
deps:
	go mod download
build:
	go build -buildvcs=false -o bin/downloader ./cmd/app
run: build
	@set -a; source "$(ENV_FILE)"; set +a; exec ./bin/downloader serve
up:
	$(COMPOSE) --env-file "$(ENV_FILE)" up -d --build --remove-orphans
down:
	$(COMPOSE) down
restart:
	$(COMPOSE) restart app
logs:
	$(COMPOSE) logs -f --tail=100 app
ps:
	$(COMPOSE) ps
fmt:
	gofmt -w cmd internal
check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...
	go test ./...
	go build -buildvcs=false ./...
health:
	@set -a; source "$(ENV_FILE)"; set +a; curl --fail --silent --show-error "http://localhost:$${HTTP_PORT:-8085}/healthz"
webhook-set:
	@ENV_FILE="$(ENV_FILE)" WEBHOOK_URL="$(WEBHOOK_URL)" bash scripts/webhook.sh setWebhook
webhook-info:
	@ENV_FILE="$(ENV_FILE)" bash scripts/webhook.sh getWebhookInfo
webhook-delete:
	@ENV_FILE="$(ENV_FILE)" bash scripts/webhook.sh deleteWebhook
front-up:
	docker compose --project-directory "$(FRONT_DIR)" -f "$(FRONT_DIR)/compose.yaml" up -d --build
front-down:
	docker compose --project-directory "$(FRONT_DIR)" -f "$(FRONT_DIR)/compose.yaml" down
front-logs:
	docker compose --project-directory "$(FRONT_DIR)" -f "$(FRONT_DIR)/compose.yaml" logs -f --tail=100
