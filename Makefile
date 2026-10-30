# Builds claudit and publishes a release: the binaries in a GitHub release of REPO, the Homebrew
# formula in TAP, so that `brew install pixelbender/tap/claudit` installs it.
#
#   make build                   the binary for this machine
#   make dist VERSION=0.1.0      tests, then tarballs for macOS and Linux and the formula in dist/
#   make release VERSION=0.1.0   dist, then the GitHub release v0.1.0 and the formula pushed to TAP
#
# release needs gh logged in with the right to push to REPO and TAP.

REPO      := pixelbender/claudit
TAP       := pixelbender/homebrew-tap
PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64
DIST      := dist
FORMULA   := Formula/claudit.rb

.PHONY: build test dist release clean version

build:
	go build -o claudit .

test:
	go vet ./...
	test -z "$$(gofmt -l .)"
	go test ./...

version:
	@test -n "$(VERSION)" || { echo "VERSION is missing: make $(MAKECMDGOALS) VERSION=0.1.0"; exit 1; }

dist: version test
	rm -rf $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*} arch=$${p#*/}; \
		echo "build $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" -o $(DIST)/$$os-$$arch/claudit . || exit 1; \
		tar -czf $(DIST)/claudit-$(VERSION)-$$os-$$arch.tar.gz -C $(DIST)/$$os-$$arch claudit || exit 1; \
	done
	packaging/formula.sh $(VERSION) $(REPO) $(DIST) > $(DIST)/claudit.rb
	@echo "formula: $(DIST)/claudit.rb"

# The release first, so that the formula never names tarballs not there yet. The formula goes
# through the GitHub API: no clone of the tap, and an empty tap gets its first commit.
release: dist
	gh release create v$(VERSION) $(DIST)/*.tar.gz --repo $(REPO) \
		--title "claudit $(VERSION)" --notes "brew install pixelbender/tap/claudit"
	sha=$$(gh api repos/$(TAP)/contents/$(FORMULA) --jq .sha 2>/dev/null) || sha=; \
	gh api -X PUT repos/$(TAP)/contents/$(FORMULA) --silent \
		-f message="claudit $(VERSION)" \
		-f content="$$(base64 < $(DIST)/claudit.rb | tr -d '\n')" \
		$${sha:+-f sha=$$sha}
	@echo "released: brew install pixelbender/tap/claudit"

clean:
	rm -rf $(DIST) claudit
