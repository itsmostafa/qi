class Qi < Formula
  desc "Local-first knowledge search CLI"
  homepage "https://github.com/itsmostafa/qi"
  url "https://github.com/itsmostafa/qi/archive/refs/tags/v0.9.0.tar.gz"
  sha256 "afd5f9aa80132dd6a8ec7cee6900755137d4a9fab869bcf1bd3b3426449a0112"
  license "MIT"
  head "https://github.com/itsmostafa/qi.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/itsmostafa/qi/internal/version.Version=v#{version}
      -X github.com/itsmostafa/qi/internal/version.BuildDate=#{time.iso8601}
    ]
    ldflags << "-X github.com/itsmostafa/qi/internal/version.Commit=#{Utils.git_short_head}" if build.head?
    system "go", "build", *std_go_args(ldflags: ldflags)
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/qi --version")
  end
end
