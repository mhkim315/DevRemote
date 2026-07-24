class Pokit < Formula
  desc "Accountless local-control CLI and daemon"
  homepage "https://github.com/mhkim315/DevRemote"
  url "https://github.com/mhkim315/DevRemote/archive/5632868c640fc7249414440b9d30102d6633dab1.tar.gz"
  version "0.0.1-dev-build10"
  sha256 "c2ff2de98b8e109b2370ed95addec46cf0741cf5bd8fb4ead985f313117a1490"
  license "MIT"

  depends_on "go" => :build

  on_macos do
    def install
      build_time = Time.now.utc.strftime("%Y-%m-%dT%H:%M:%SZ")
      ldflags = "-X main.cliVersion=#{version} -X main.cliGitSHA=5632868c640fc7249414440b9d30102d6633dab1 -X main.cliBuildTime=#{build_time}"
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
