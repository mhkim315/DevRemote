class Pokit < Formula
  desc "Accountless local-control CLI and daemon"
  homepage "https://github.com/mhkim315/DevRemote"
  url "https://github.com/mhkim315/DevRemote/archive/363ec91fd0dc5b0d52190630531af1a1a2d5d380.tar.gz"
  version "0.0.1-dev-build9"
  sha256 "35d00d8341a6aa02c3e5700e5faee1f95cd146fcdfab96527dce8e75dcd1d204"
  license "MIT"

  depends_on "go" => :build

  on_macos do
    def install
      build_time = Time.now.utc.strftime("%Y-%m-%dT%H:%M:%SZ")
      ldflags = "-X main.cliVersion=#{version} -X main.cliGitSHA=363ec91fd0dc5b0d52190630531af1a1a2d5d380 -X main.cliBuildTime=#{build_time}"
      cd "companion-daemon" do
        system "go", "build", "-trimpath", "-ldflags", ldflags, "-o", bin/"pokit", "./cmd/devremote"
      end
    end
  end

  def caveats
    <<~EOS
      The formula installs only the pokit CLI/daemon binary.
      Install and manage the per-user LaunchAgent explicitly:
        pokit doctor
        pokit daemon install
        pokit daemon start
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/pokit doctor")
  end
end
