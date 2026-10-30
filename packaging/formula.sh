#!/bin/sh
# formula.sh VERSION REPO DIST prints the Homebrew formula of claudit VERSION: it installs the
# tarballs `make dist` put in DIST, downloaded from the release vVERSION of the GitHub repo REPO.
set -eu
version=$1 repo=$2 dist=$3

url() { echo "https://github.com/$repo/releases/download/v$version/claudit-$version-$1.tar.gz"; }
sha() { shasum -a 256 "$dist/claudit-$version-$1.tar.gz" | cut -d ' ' -f 1; }

cat <<EOF
class Claudit < Formula
  desc "Terminal UI that shows Claude Code's agents at work in a make-believe IDE"
  homepage "https://github.com/$repo"
  version "$version"
  license "MIT"

  on_macos do
    on_arm do
      url "$(url darwin-arm64)"
      sha256 "$(sha darwin-arm64)"
    end
    on_intel do
      url "$(url darwin-amd64)"
      sha256 "$(sha darwin-amd64)"
    end
  end

  on_linux do
    on_arm do
      url "$(url linux-arm64)"
      sha256 "$(sha linux-arm64)"
    end
    on_intel do
      url "$(url linux-amd64)"
      sha256 "$(sha linux-amd64)"
    end
  end

  def install
    bin.install "claudit"
  end

  def caveats
    <<~EOS
      claudit runs Claude Code, which must be on the PATH and logged in:
        brew install --cask claude-code
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/claudit --version")
  end
end
EOF
