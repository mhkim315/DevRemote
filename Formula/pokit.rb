class Pokit < Formula
  desc "Accountless local-control CLI and daemon"
  homepage "https://github.com/mhkim315/DevRemote"
  url "https://github.com/mhkim315/DevRemote/archive/ebabf68326271a150e11cba986d286c03d533460.tar.gz"
  version "0.0.1-dev-build10"
  sha256 "f11677658b843114964fc840453395d150b562022b93616705253dd8cc9ed403"
  license "MIT"

  depends_on "go" => :build

  on_macos do
    def install
      build_time = Time.now.utc.strftime("%Y-%m-%dT%H:%M:%SZ")
      ldflags = "-X main.cliVersion=#{version} -X main.cliGitSHA=ebabf68326271a150e11cba986d286c03d533460 -X main.cliBuildTime=#{build_time}"
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
