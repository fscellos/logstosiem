build:
	go build -x -o bin/logtosiem

docker-build:
	eval $(minikube -p nauarkhos docker-env)
	docker build -t logtosiem .

deploy:
	kubectl apply -k kubernetes/base

build-all: build docker-build