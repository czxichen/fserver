GOBUILD=go build -v
Version=0.0.0

#使用version指定版本
ifdef version
	Version=$(version)
endif

LDFLAGS += -X "main.GitHash=$(shell git rev-parse HEAD)"
LDFLAGS += -X "main.Version=$(Version)"

all: build

.PHONY : clean build

build:  
	$(GOBUILD) -ldflags '-s -w $(LDFLAGS)' -i cmd/fserver.go
