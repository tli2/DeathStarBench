.PHONY: proto data run

proto:
	for f in services/**/proto/*.proto; do \
		protoc --go-grpc_out=. $$f; \
		echo compiled: $$f; \
	done

data:
	go-bindata -o data/bindata.go -pkg data data/*.json

run:
	docker-compose build
	docker-compose up --remove-orphans
