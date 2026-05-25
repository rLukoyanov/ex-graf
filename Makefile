REGISTRY ?= docker.io/youruser
APP_NAME ?= influx-app
VERSION ?= latest

.PHONY: build docker-build docker-push up down logs ps clean k8s-apply k8s-delete

build:
	go build -o bin/server ./cmd/server

docker-build:
	docker compose build app

docker-push:
	docker tag influx-app:latest $(REGISTRY)/$(APP_NAME):$(VERSION)
	docker push $(REGISTRY)/$(APP_NAME):$(VERSION)

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

ps:
	docker compose ps

clean:
	rm -rf bin/

k8s-apply:
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/app/configmap.yaml
	kubectl apply -f k8s/app/deployment.yaml
	kubectl apply -f k8s/app/service.yaml
	kubectl apply -f k8s/influxdb/pvc.yaml
	kubectl apply -f k8s/influxdb/deployment.yaml
	kubectl apply -f k8s/influxdb/service.yaml
	kubectl apply -f k8s/grafana/pvc.yaml
	kubectl apply -f k8s/grafana/configmap.yaml
	kubectl apply -f k8s/grafana/deployment.yaml
	kubectl apply -f k8s/grafana/service.yaml
	kubectl apply -f k8s/ingress.yaml

k8s-delete:
	kubectl delete -f k8s/ingress.yaml --ignore-not-found
	kubectl delete -f k8s/grafana/service.yaml --ignore-not-found
	kubectl delete -f k8s/grafana/deployment.yaml --ignore-not-found
	kubectl delete -f k8s/grafana/pvc.yaml --ignore-not-found
	kubectl delete -f k8s/influxdb/service.yaml --ignore-not-found
	kubectl delete -f k8s/influxdb/deployment.yaml --ignore-not-found
	kubectl delete -f k8s/influxdb/pvc.yaml --ignore-not-found
	kubectl delete -f k8s/app/service.yaml --ignore-not-found
	kubectl delete -f k8s/app/deployment.yaml --ignore-not-found
	kubectl delete -f k8s/app/configmap.yaml --ignore-not-found
	kubectl delete -f k8s/namespace.yaml --ignore-not-found
