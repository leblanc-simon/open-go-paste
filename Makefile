VERSION := $(shell git tag | tail -n1)
MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
CURRENT_DIR := $(patsubst %/,%,$(dir $(MKFILE_PATH)))
OUTDIR := ${CURRENT_DIR}/release/${VERSION}

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

$(OUTDIR):
	rm -fr $(OUTDIR) || true
	mkdir -p $(OUTDIR)

debug: ## Build a debug version
	cd $(CURRENT_DIR)/server; go build -o open-go-paste .

prod: $(OUTDIR) ## Build a production version
	cd $(CURRENT_DIR)/server; go build -ldflags "-s -w" -o $(OUTDIR)/server/open-go-paste .

# The templates and the assets are embedded in the binary, only the paste
# storage has to be shipped alongside it.
release: prod ## Generate a production release
	mkdir -p $(OUTDIR)/datas
	tar -cjf $(OUTDIR)/../open-go-paste-$(VERSION).tar.bz2 -C $(OUTDIR) .
	rm -fr $(OUTDIR) || true

.PHONY: help
.DEFAULT_GOAL := help
.SHELLFLAGS += -e
