class Altcode < Formula
  desc "AI-assisted coding CLI — multi-provider, Claude Code compatible"
  homepage "https://github.com/jiayaoqijia/altcode"
  version "0.5.0"
  license "AGPL-3.0"

  on_macos do
    on_arm do
      url "https://github.com/jiayaoqijia/altcode/releases/download/v0.5.0/altcode-darwin-arm64"
      sha256 "" # TODO: fill after release
    end
    on_intel do
      url "https://github.com/jiayaoqijia/altcode/releases/download/v0.5.0/altcode-darwin-amd64"
      sha256 "" # TODO: fill after release
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/jiayaoqijia/altcode/releases/download/v0.5.0/altcode-linux-arm64"
      sha256 "" # TODO: fill after release
    end
    on_intel do
      url "https://github.com/jiayaoqijia/altcode/releases/download/v0.5.0/altcode-linux-amd64"
      sha256 "" # TODO: fill after release
    end
  end

  def install
    binary = Dir["altcode-*"].first || "altcode"
    bin.install binary => "altcode"
  end

  test do
    assert_match "altcode version", shell_output("#{bin}/altcode --version")
  end
end
