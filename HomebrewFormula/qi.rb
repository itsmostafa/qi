class Qi < Formula
  desc "Local-first knowledge search CLI"
  homepage "https://github.com/itsmostafa/qi"
  url "https://github.com/itsmostafa/qi/archive/refs/tags/v0.8.0.tar.gz"
  sha256 "a6549b17c7a666ef17ef4ad84f7fcaa3a4fdd4a28054994987454244ccb93a04"
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
