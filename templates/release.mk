NPM_PACKAGE ?= mcp-template-npm-package-placeholder
NPM_PACKAGE_DIRECTORY ?= __MCP_NPM_DIRECTORY__
NPM_ROOT ?= npm
NPM_VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
NPM_PUBLISH_FLAGS ?=
NPM_PACK_DIR ?= dist/npm-packages
NPM ?= npm
NODE ?= node

.PHONY: npm-verify-binaries npm-prepare-packages npm-create-tarballs npm-publish

npm-verify-binaries:
	@set -eu; \
	for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE_DIRECTORY)-*; do \
		test -d "$$package_dir"; \
		if test -f "$$package_dir/bin/$(BINARY_NAME)" || test -f "$$package_dir/bin/$(BINARY_NAME).exe"; then continue; fi; \
		printf '%s\n' "Missing binary in $$package_dir" >&2; exit 1; \
	done

npm-prepare-packages: npm-verify-binaries
	test -f README.md
	test -f LICENSE
	cp README.md LICENSE "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)/"
	@set -eu; for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE_DIRECTORY)-*; do \
		jq --arg version "$(NPM_VERSION)" '.version = $$version' "$$package_dir/package.json" > "$$package_dir/package.json.tmp"; \
		mv "$$package_dir/package.json.tmp" "$$package_dir/package.json"; \
	done
	jq --arg version "$(NPM_VERSION)" '.version = $$version | .optionalDependencies |= with_entries(.value = $$version)' "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)/package.json" > "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)/package.json.tmp"
	mv "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)/package.json.tmp" "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)/package.json"

npm-create-tarballs: npm-prepare-packages
	rm -rf "$(NPM_PACK_DIR)"
	mkdir -p "$(NPM_PACK_DIR)"
	@set -eu; for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE_DIRECTORY)-* "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)"; do \
		filename="$$(cd "$$package_dir" && $(NPM) pack --json | jq -er '.[0].filename')"; \
		mv "$$package_dir/$$filename" "$(NPM_PACK_DIR)/$$(basename "$$package_dir").tgz"; \
	done

npm-publish: npm-create-tarballs
	@set -eu; \
	temp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$temp_dir"' EXIT; \
	for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE_DIRECTORY)-* "$(NPM_ROOT)/$(NPM_PACKAGE_DIRECTORY)"; do \
		package_name="$$(jq -er '.name' "$$package_dir/package.json")"; \
		package_version="$$(jq -er '.version' "$$package_dir/package.json")"; \
		package_spec="$$package_name@$$package_version"; \
		archive="$(NPM_PACK_DIR)/$$(basename "$$package_dir").tgz"; \
		local_integrity="$$( $(NODE) -e 'const crypto = require("crypto"); const fs = require("fs"); process.stdout.write("sha512-" + crypto.createHash("sha512").update(fs.readFileSync(process.argv[1])).digest("base64"));' "$$archive")"; \
		if $(NPM) view "$$package_spec" dist.integrity --json > "$$temp_dir/view.json" 2> "$$temp_dir/view.err"; then \
			remote_integrity="$$(jq -er 'if type == "string" and length > 0 then . else error("missing dist.integrity") end' "$$temp_dir/view.json")"; \
			if [ "$$local_integrity" != "$$remote_integrity" ]; then printf '%s\n' "Existing $$package_spec does not match the local release artifact" >&2; exit 1; fi; \
			echo "$$package_spec is already published with matching integrity"; \
		elif grep -Eq 'E404|404 Not Found' "$$temp_dir/view.err"; then \
			NPM_PACKAGE_NAME="$$package_name" NPM_PACKAGE_VERSION="$$package_version" $(NPM) publish "$$archive" $(NPM_PUBLISH_FLAGS); \
		else \
			cat "$$temp_dir/view.err" >&2; exit 1; \
		fi; \
	done
