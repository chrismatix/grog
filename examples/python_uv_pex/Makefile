.PHONY: contents build-pex build-images test

contents:
build-pex:
	@for dir in $$(./scripts/get_targets.py); do \
		if [ -n "$$PLATFORM" ]; then \
			./scripts/build_pex_in_docker.sh $$dir $$PLATFORM; \
		else \
			./scripts/build_pex.sh $$dir; \
		fi; \
		echo ""; \
	done

build-images: build-pex
	@for dir in $$(./scripts/get_targets.py); do \
		echo "Building image for target $$dir..."; \
		earthly $${PLATFORM:+--platform $$PLATFORM} ./$$dir+build; \
		echo ""; \
	done

test:
	@for dir in $$(./scripts/get_workspaces.py); do \
		echo "Testing target $$dir..." && \
		(cd $$dir && uv run pytest) || exit 1; \
		echo ""; \
	done
