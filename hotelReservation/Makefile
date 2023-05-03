.PHONY: proto data run

proto:
	for f in services/**/proto/**.proto; do \
		/home/arielck/workspace/projects/research/oldproto/bin/protoc --go_out=plugins=grpc:. $$f; \
		echo compiled: $$f; \
	done


#/home/arielck/workspace/projects/research/oldproto/bin/protoc --go-grpc_out=. $$f; \

data:
	go-bindata -o data/bindata.go -pkg data data/*.json

run:
	docker-compose build
	docker-compose up --remove-orphans
