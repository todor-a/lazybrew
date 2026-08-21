class Lazybrew < Formula
  desc "Terminal UI for inspecting and uninstalling Homebrew packages"
  homepage "https://github.com/todor-a/lazybrew"
  url "https://github.com/todor-a/lazybrew/releases/download/v@VERSION@/lazybrew-@VERSION@.tar.gz"
  version "@VERSION@"
  sha256 "@SHA256@"

  depends_on :macos
  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/lazybrew"
  end

  test do
    output = shell_output("#{bin}/lazybrew 2>&1", 1)
    assert_equal "lazybrew requires an interactive terminal\n", output
  end
end
