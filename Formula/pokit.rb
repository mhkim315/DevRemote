class Pokit < Formula
  desc "Accountless local-control CLI and daemon"
  homepage "https://github.com/mhkim315/DevRemote"
  url "https://github.com/mhkim315/DevRemote/archive/4e36f97b190157876b54b6403b9a8b9374393f58.tar.gz"
  version "0.0.1-dev-build10"
  sha256 "b0bdf37514f89ca112bcb73976e14bfec75e1c9fb132da57765c163c041c8cad"
  license "MIT"

  depends_on "go" => :build

  on_macos do
    def install
      build_time = Time.now.utc.strftime("%Y-%m-%dT%H:%M:%SZ")
      ldflags = "-X main.cliVersion=#{version} -X main.cliGitSHA=4e36f97b190157876b54b6403b9a8b9374393f58 -X main.cliBuildTime=#{build_time}"
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
